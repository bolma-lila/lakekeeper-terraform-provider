//go:build acceptance

package provider

import (
	"fmt"
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/lakekeeper/terraform-provider-lakekeeper/internal/provider/testutil"
)

func TestAccDataLakekeeperNamespaces_basic(t *testing.T) {
	project := testutil.CreateProject(t)
	warehouse := testutil.CreateWarehouse(t, project.ID, "test-data-namespaces")

	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: fmt.Sprintf(`
resource "lakekeeper_namespace" "parent" {
  project_id     = %q
  warehouse_name = %q
  name           = "parent"
}

resource "lakekeeper_namespace" "child" {
  project_id     = %q
  warehouse_name = %q
  name           = "parent.child"
  depends_on     = [lakekeeper_namespace.parent]
}

data "lakekeeper_namespaces" "roots" {
  warehouse_id = %q
  depends_on   = [lakekeeper_namespace.parent]
}

data "lakekeeper_namespaces" "children" {
  warehouse_id = %q
  parent       = ["parent"]
  depends_on   = [lakekeeper_namespace.child]
}
`, project.ID, warehouse.Name, project.ID, warehouse.Name, warehouse.ID, warehouse.ID),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("data.lakekeeper_namespaces.roots", "warehouse_id", warehouse.ID),
					resource.TestCheckResourceAttr("data.lakekeeper_namespaces.roots", "namespaces.#", "1"),
					resource.TestCheckResourceAttr("data.lakekeeper_namespaces.roots", "namespaces.0.parts.#", "1"),
					resource.TestCheckResourceAttr("data.lakekeeper_namespaces.roots", "namespaces.0.parts.0", "parent"),
					resource.TestCheckResourceAttr("data.lakekeeper_namespaces.children", "namespaces.#", "1"),
					resource.TestCheckResourceAttr("data.lakekeeper_namespaces.children", "namespaces.0.parts.#", "2"),
					resource.TestCheckResourceAttr("data.lakekeeper_namespaces.children", "namespaces.0.parts.0", "parent"),
					resource.TestCheckResourceAttr("data.lakekeeper_namespaces.children", "namespaces.0.parts.1", "child"),
				),
			},
		},
	})
}

// The parent levels are checked before any request: a null or an empty level
// fails validation.
func TestAccDataLakekeeperNamespaces_invalidParent(t *testing.T) {
	config := func(parent string) string {
		return fmt.Sprintf(`
		data "lakekeeper_namespaces" "test" {
			warehouse_id = "22222222-2222-4222-8222-222222222222"
			parent       = %s
		}`, parent)
	}

	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{Config: config(`["a", null]`), ExpectError: regexp.MustCompile(`Null List Value`)},
			{Config: config(`["a", ""]`), ExpectError: regexp.MustCompile(`string length must be at least 1`)},
		},
	})
}
