package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/lakekeeper/terraform-provider-lakekeeper/internal/provider/api"
)

func TestRoleNameLookup(t *testing.T) {
	roleJSON := func(id, projectID, name string) string {
		return fmt.Sprintf(`{"id":%q,"project-id":%q,"name":%q,"created-at":"2026-10-30T10:00:00Z","ident":"lakekeeper~%s","provider-id":"lakekeeper","source-id":%q}`, id, projectID, name, id, id)
	}
	roles := func(r ...string) string { return `{"roles":[` + strings.Join(r, ",") + `]}` }
	for _, tc := range []struct {
		name string
		// search is the search response; pages are the list pages, read only
		// when the search holds no match.
		search    string
		pages     []string
		wantError string
	}{
		{"search finds it", roles(roleJSON("r", "p", "target")), nil, ""},
		{"search finds another case", roles(roleJSON("r", "p", "Target")), nil, ""},
		{"search finds two", roles(roleJSON("r", "p", "target"), roleJSON("s", "p", "TARGET")), nil, "multiple roles"},
		{"list finds what search missed", roles(roleJSON("x", "p", "target-extra")), []string{roles(roleJSON("r", "p", "target"))}, ""},
		{"missing", roles(), []string{roles()}, "no role"},
		{"prefix is not exact", roles(roleJSON("x", "p", "target-extra")), []string{roles(roleJSON("x", "p", "target-extra"))}, "no role"},
		{"other project is skipped", roles(roleJSON("r", "other", "target")), []string{roles(roleJSON("r", "other", "target"))}, "no role"},
		{"second page", roles(), []string{`{"roles":[],"next-page-token":"next"}`, roles(roleJSON("r", "p", "target"))}, ""},
		{"ambiguous across pages", roles(), []string{`{"roles":[` + roleJSON("r", "p", "target") + `],"next-page-token":"next"}`, roles(roleJSON("s", "p", "target"))}, "multiple roles"},
		{"repeated token", roles(), []string{`{"roles":[],"next-page-token":"next"}`, `{"roles":[],"next-page-token":"next"}`}, "repeated"},
		{"later page fails", roles(), []string{`{"roles":[` + roleJSON("r", "p", "target") + `],"next-page-token":"next"}`, "error"}, "list roles"},
		{"search fails", "error", nil, "search roles"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				if r.Header.Get("x-project-id") != "p" {
					t.Errorf("missing project header: %s %s", r.Method, r.URL)
				}
				page := tc.search
				switch r.Method + " " + r.URL.Path {
				case "POST /management/v1/search/role":
					if body, _ := io.ReadAll(r.Body); !strings.Contains(string(body), `"search":"target"`) {
						t.Errorf("unexpected search %s", body)
					}
				case "GET /management/v1/role":
					if calls > 0 && r.URL.Query().Get("pageToken") != "next" {
						t.Error("missing pagination token")
					}
					if calls >= len(tc.pages) {
						t.Error("unexpected list page")
						w.WriteHeader(500)
						return
					}
					page = tc.pages[calls]
					calls++
				default:
					t.Errorf("unexpected request %s %s", r.Method, r.URL)
					w.WriteHeader(500)
					return
				}
				if page == "error" {
					w.WriteHeader(403)
					_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{"message": "denied", "type": "Forbidden", "code": 403}})
					return
				}
				_, _ = w.Write([]byte(page))
			}))
			defer server.Close()
			client, err := api.NewStaticTokenClient(context.Background(), server.URL, "test")
			if err != nil {
				t.Fatal(err)
			}
			role, err := findRoleByName(context.Background(), client, "p", "target")
			if tc.wantError != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantError) {
					t.Fatalf("wanted %q, got %v", tc.wantError, err)
				}
			} else if err != nil || role.ID != "r" {
				t.Fatalf("role=%v err=%v", role, err)
			}
		})
	}
}

func TestRoleReadByIDAndName(t *testing.T) {
	for _, byName := range []bool{false, true} {
		t.Run(fmt.Sprint(byName), func(t *testing.T) {
			roleJSON := `{"id":"r","project-id":"p","name":"target","created-at":"2026-10-30T10:00:00Z","ident":"lakekeeper~r","provider-id":"lakekeeper","source-id":"r"}`
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				if byName {
					if r.URL.Path != "/management/v1/search/role" {
						t.Errorf("unexpected name lookup %s", r.URL.Path)
					}
					_, _ = w.Write([]byte(`{"roles":[` + roleJSON + `]}`))
				} else {
					if r.URL.Path != "/management/v1/role/r" {
						t.Errorf("unexpected ID lookup %s", r.URL.Path)
					}
					_, _ = w.Write([]byte(roleJSON))
				}
			}))
			defer server.Close()
			client, err := api.NewStaticTokenClient(context.Background(), server.URL, "test")
			if err != nil {
				t.Fatal(err)
			}
			d := &lakekeeperRoleDataSource{client: client}
			var schemaResp datasource.SchemaResponse
			d.Schema(context.Background(), datasource.SchemaRequest{}, &schemaResp)
			state := tfsdk.State{Schema: schemaResp.Schema}
			input := lakekeeperRoleDataSourceModel{ProjectID: types.StringValue("p")}
			wantName := "target"
			if byName {
				// A configured name keeps its spelling even when Lakekeeper stores another case.
				wantName = "Target"
				input.Name = types.StringValue(wantName)
			} else {
				input.RoleID = types.StringValue("r")
			}
			if diags := state.Set(context.Background(), input); diags.HasError() {
				t.Fatal(diags)
			}
			resp := datasource.ReadResponse{State: state}
			d.Read(context.Background(), datasource.ReadRequest{Config: tfsdk.Config{Schema: schemaResp.Schema, Raw: state.Raw}}, &resp)
			if resp.Diagnostics.HasError() {
				t.Fatal(resp.Diagnostics)
			}
			var result lakekeeperRoleDataSourceModel
			if diags := resp.State.Get(context.Background(), &result); diags.HasError() {
				t.Fatal(diags)
			}
			if result.ID.ValueString() != "p/r" || result.RoleID.ValueString() != "r" || result.Name.ValueString() != wantName {
				t.Fatalf("incorrect role state: %+v", result)
			}
		})
	}
}
