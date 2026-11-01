# Store

Read [Store items: installable tools and flows](../development/extending.md#store-items-installable-tools-and-flows)
for the current installation paths, manifests, version records and user
permission rules.

The code supports staging and validating external skills, installing local
extension directories, validating and activating tool packages, and running
versioned automation workflows. The store capability currently uses a
deterministic stand-in that records install and publish requests.

A production remote store backend, populated published catalog and general
remote tool/flow installer are not built here. The `store/catalog`,
`store/examples` and `store/installer` directories are scaffolding. Installing,
creating or publishing an item requires an explicit user request; ask first
when authorization is absent.
