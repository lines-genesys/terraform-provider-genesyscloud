---
# generated by https://github.com/hashicorp/terraform-plugin-docs
page_title: "genesyscloud_auth_role Data Source - terraform-provider-genesyscloud"
subcategory: ""
description: |-
  Data source for Genesys Cloud Roles. Select a role by name.
---

# genesyscloud_auth_role (Data Source)

Data source for Genesys Cloud Roles. Select a role by name.

## Example Usage

```terraform
data "genesyscloud_auth_role" "employee" {
  name = "employee"
}
```

<!-- schema generated by tfplugindocs -->
## Schema

### Required

- `name` (String) Role name.

### Read-Only

- `id` (String) The ID of this resource.


