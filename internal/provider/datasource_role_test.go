//go:build acceptance

package provider

import (
	"fmt"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/hashicorp/terraform-plugin-testing/helper/acctest"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/lakekeeper/terraform-provider-lakekeeper/internal/provider/testutil"
)

func TestAccDataLakekeeperRole_basic(t *testing.T) {

	project := testutil.CreateProject(t)
	defaultProject, err := testutil.TestLakekeeperClient.GetProject(t.Context(), uuid.Nil.String())
	if err != nil {
		t.Fatalf("could not get default project, %v", err)
	}

	roleDefaultProject := testutil.CreateRole(t, defaultProject.ID)
	roleNewProject := testutil.CreateRole(t, project.ID)

	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: fmt.Sprintf(`
				data "lakekeeper_default_project" "default" {}
				data "lakekeeper_role" "default" {
					project_id = data.lakekeeper_default_project.default.id
					role_id = "%s"
				}
				data "lakekeeper_role" "new" {
					role_id = "%s"
					project_id = "%s"
				}`, roleDefaultProject.ID, roleNewProject.ID, project.ID),
				Check: resource.ComposeAggregateTestCheckFunc(
					// role in default project
					resource.TestCheckResourceAttr("data.lakekeeper_role.default", "id", "00000000-0000-0000-0000-000000000000/"+roleDefaultProject.ID),
					resource.TestCheckResourceAttr("data.lakekeeper_role.default", "role_id", roleDefaultProject.ID),
					resource.TestCheckResourceAttr("data.lakekeeper_role.default", "project_id", "00000000-0000-0000-0000-000000000000"),
					resource.TestCheckResourceAttr("data.lakekeeper_role.default", "name", roleDefaultProject.Name),
					resource.TestCheckResourceAttr("data.lakekeeper_role.default", "description", *roleDefaultProject.Description),
					resource.TestCheckResourceAttr("data.lakekeeper_role.default", "created_at", roleDefaultProject.CreatedAt),

					// role in created project
					resource.TestCheckResourceAttr("data.lakekeeper_role.new", "id", fmt.Sprintf("%s/%s", project.ID, roleNewProject.ID)),
					resource.TestCheckResourceAttr("data.lakekeeper_role.new", "role_id", roleNewProject.ID),
					resource.TestCheckResourceAttr("data.lakekeeper_role.new", "project_id", project.ID),
					resource.TestCheckResourceAttr("data.lakekeeper_role.new", "name", roleNewProject.Name),
					resource.TestCheckResourceAttr("data.lakekeeper_role.new", "description", *roleNewProject.Description),
					resource.TestCheckResourceAttr("data.lakekeeper_role.new", "created_at", roleNewProject.CreatedAt),
				),
			},
		},
	})
}

func TestAccDataLakekeeperRole_byName(t *testing.T) {
	project := testutil.CreateProject(t)
	roleName := "test-role-" + acctest.RandString(8)
	config := func(name string) string {
		return fmt.Sprintf(`
		resource "lakekeeper_role" "test" {
			project_id = %[1]q
			name       = %[2]q
		}
		data "lakekeeper_role" "by_name" {
			project_id = %[1]q
			name       = %[3]s
		}`, project.ID, roleName, name)
	}

	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: config("lakekeeper_role.test.name"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("data.lakekeeper_role.by_name", "project_id", project.ID),
					resource.TestCheckResourceAttr("data.lakekeeper_role.by_name", "name", roleName),
					resource.TestCheckResourceAttrPair("data.lakekeeper_role.by_name", "role_id", "lakekeeper_role.test", "role_id"),
					resource.TestCheckResourceAttrPair("data.lakekeeper_role.by_name", "id", "lakekeeper_role.test", "id"),
				),
			},
			// A name in another case finds the same role and keeps its configured spelling.
			{
				Config: config("upper(lakekeeper_role.test.name)"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("data.lakekeeper_role.by_name", "name", strings.ToUpper(roleName)),
					resource.TestCheckResourceAttrPair("data.lakekeeper_role.by_name", "role_id", "lakekeeper_role.test", "role_id"),
				),
			},
		},
	})
}

func TestAccDataLakekeeperRole_byID(t *testing.T) {
	project := testutil.CreateProject(t)
	roleName := "test-role-" + acctest.RandString(8)

	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: fmt.Sprintf(`
				resource "lakekeeper_role" "test" {
					project_id = %[1]q
					name       = %[2]q
				}
				data "lakekeeper_role" "by_id" {
					project_id = %[1]q
					role_id    = lakekeeper_role.test.role_id
				}`, project.ID, roleName),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("data.lakekeeper_role.by_id", "project_id", project.ID),
					resource.TestCheckResourceAttr("data.lakekeeper_role.by_id", "name", roleName),
					resource.TestCheckResourceAttrPair("data.lakekeeper_role.by_id", "role_id", "lakekeeper_role.test", "role_id"),
					resource.TestCheckResourceAttrPair("data.lakekeeper_role.by_id", "id", "lakekeeper_role.test", "id"),
				),
			},
			// An upper-case role ID finds the same role.
			{
				Config: fmt.Sprintf(`
				resource "lakekeeper_role" "test" {
					project_id = %[1]q
					name       = %[2]q
				}
				data "lakekeeper_role" "by_id" {
					project_id = %[1]q
					role_id    = upper(lakekeeper_role.test.role_id)
				}`, project.ID, roleName),
				Check: resource.TestCheckResourceAttr("data.lakekeeper_role.by_id", "name", roleName),
			},
		},
	})
}
