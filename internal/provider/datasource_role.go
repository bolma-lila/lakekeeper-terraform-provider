package provider

import (
	"context"
	"fmt"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/lakekeeper/terraform-provider-lakekeeper/internal/provider/api"
)

// Ensure the implementation satisfies the expected interfaces.
var (
	_ datasource.DataSource              = &lakekeeperRoleDataSource{}
	_ datasource.DataSourceWithConfigure = &lakekeeperRoleDataSource{}
)

func init() {
	registerDataSource(NewLakekeeperRoleDataSource)
}

// NewLakekeeperRoleDataSource is a helper function to simplify the provider implementation.
func NewLakekeeperRoleDataSource() datasource.DataSource {
	return &lakekeeperRoleDataSource{}
}

// lakekeeperRoleDataSource is the data source implementation.
type lakekeeperRoleDataSource struct {
	client *api.Client
}

// lakekeeperRoleDataSourceModel describes the data source data model.
type lakekeeperRoleDataSourceModel struct {
	ID          types.String `tfsdk:"id"`
	RoleID      types.String `tfsdk:"role_id"`
	ProjectID   types.String `tfsdk:"project_id"`
	Name        types.String `tfsdk:"name"`
	Description types.String `tfsdk:"description"`

	CreatedAt types.String `tfsdk:"created_at"`
	UpdatedAt types.String `tfsdk:"updated_at"`
}

// Metadata returns the data source type name.
func (d *lakekeeperRoleDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_role"
}

// Schema defines the schema for the data source.
func (d *lakekeeperRoleDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: fmt.Sprintf(`The ` + "`lakekeeper_role`" + ` data source retrieves information a lakekeeper role.

**Upstream API**: [Lakekeeper REST API docs](https://docs.lakekeeper.io/docs/nightly/api/management/#tag/role/operation/get_role)`),

		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "The ID of the role. in the form `{{project_id}}/{{role_id}}`",
				Computed:            true,
			},
			"role_id": schema.StringAttribute{
				MarkdownDescription: `The internal ID of the role. Configure exactly one of role_id or name.`,
				Optional:            true,
				Computed:            true,
				Validators: []validator.String{
					stringvalidator.LengthAtLeast(1),
					stringvalidator.ExactlyOneOf(path.MatchRoot("name")),
				},
			},
			"project_id": schema.StringAttribute{
				MarkdownDescription: `The ID of the project the role belongs to.`,
				Required:            true,
			},
			"name": schema.StringAttribute{
				MarkdownDescription: "Role name within project_id. Matching ignores case, as Lakekeeper does for role names, and `name` keeps the configured spelling. A name lookup needs one visible match and permission to search the project's roles, and to list them when the search finds no match. Configure exactly one of role_id or name.",
				Optional:            true,
				Computed:            true,
				Validators:          []validator.String{stringvalidator.LengthAtLeast(1)},
			},
			"description": schema.StringAttribute{
				MarkdownDescription: "The description of the role.",
				Computed:            true,
			},
			"created_at": schema.StringAttribute{
				MarkdownDescription: "When the role has been created.",
				Computed:            true,
			},
			"updated_at": schema.StringAttribute{
				MarkdownDescription: "When the role has last been modified.",
				Computed:            true,
			},
		},
	}
}

// Configure adds the provider configured client to the data source.
func (d *lakekeeperRoleDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, _ *datasource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}

	datasource := req.ProviderData.(*LakekeeperDatasourceData)
	d.client = datasource.Client
}

// Read refreshes the Terraform state with the latest data.
func (d *lakekeeperRoleDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var state lakekeeperRoleDataSourceModel

	// Read Terraform plan data into the model
	resp.Diagnostics.Append(req.Config.Get(ctx, &state)...)

	if resp.Diagnostics.HasError() {
		return
	}

	projectID := state.ProjectID.ValueString()

	var role *api.Role
	var err error
	var selector string
	if state.RoleID.IsNull() {
		selector = fmt.Sprintf("named %q", state.Name.ValueString())
		role, err = findRoleByName(ctx, d.client, projectID, state.Name.ValueString())
	} else {
		selector = state.RoleID.ValueString()
		role, err = d.client.GetRole(ctx, projectID, selector)
	}
	if err != nil {
		resp.Diagnostics.AddError("Lakekeeper API error occurred", fmt.Sprintf("Unable to read role %s in project %s, %v", selector, projectID, err))
		return
	}
	state.ID = types.StringValue(fmt.Sprintf("%s/%s", role.ProjectID, role.ID))
	state.RoleID = types.StringValue(role.ID)
	state.ProjectID = types.StringValue(role.ProjectID)
	if state.Name.IsNull() {
		state.Name = types.StringValue(role.Name)
	}
	state.CreatedAt = types.StringValue(role.CreatedAt)

	state.Description = types.StringPointerValue(role.Description)
	state.UpdatedAt = types.StringPointerValue(role.UpdatedAt)

	diags := resp.State.Set(ctx, &state)
	resp.Diagnostics.Append(diags...)
}

// findRoleByName returns the project's role whose name matches name, ignoring
// case. Lakekeeper's role search returns only its best fuzzy matches, so when
// they hold no match, findRoleByName pages through every role of the project
// to be sure the name is absent.
func findRoleByName(ctx context.Context, client *api.Client, projectID, name string) (*api.Role, error) {
	found, err := client.SearchRoles(ctx, projectID, name)
	if err != nil {
		return nil, fmt.Errorf("search roles: %w", err)
	}
	if match, err := matchRoleName(found, projectID, name); match != nil || err != nil {
		return match, err
	}
	var all []*api.Role
	seenTokens := map[string]bool{}
	var nextPageToken *string
	for {
		page, err := client.ListRoles(ctx, projectID, nextPageToken)
		if err != nil {
			return nil, fmt.Errorf("list roles: %w", err)
		}
		all = append(all, page.Roles...)
		if page.NextPageToken == nil {
			break
		}
		token := *page.NextPageToken
		if seenTokens[token] {
			return nil, fmt.Errorf("role list repeated a page token")
		}
		seenTokens[token] = true
		nextPageToken = &token
	}
	match, err := matchRoleName(all, projectID, name)
	if err == nil && match == nil {
		err = fmt.Errorf("no role matches")
	}
	return match, err
}

// matchRoleName returns the role in roles whose name matches name, ignoring
// case, or nil when none does. It skips roles of other projects, and fails when
// more than one role matches.
func matchRoleName(roles []*api.Role, projectID, name string) (*api.Role, error) {
	var match *api.Role
	for _, role := range roles {
		if !strings.EqualFold(role.ProjectID, projectID) || !strings.EqualFold(role.Name, name) {
			continue
		}
		if match != nil && match.ID != role.ID {
			return nil, fmt.Errorf("multiple roles match; use role_id")
		}
		match = role
	}
	return match, nil
}
