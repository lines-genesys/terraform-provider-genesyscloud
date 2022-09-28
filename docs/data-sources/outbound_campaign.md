---
# generated by https://github.com/hashicorp/terraform-plugin-docs
page_title: "genesyscloud_outbound_campaign Data Source - terraform-provider-genesyscloud"
subcategory: ""
description: |-
  Data source for Genesys Cloud Outbound Campaign. Select a Outbound Campaign by name.
---

# genesyscloud_outbound_campaign (Data Source)

Data source for Genesys Cloud Outbound Campaign. Select a Outbound Campaign by name.

## Example Usage

```terraform
data "genesyscloud_outbound_campaign" "campaign" {
  name = "Example Voice Campaign"
}
```

<!-- schema generated by tfplugindocs -->
## Schema

### Optional

- `name` (String) Outbound Campaign name.

### Read-Only

- `id` (String) The ID of this resource.

