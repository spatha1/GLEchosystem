# gl module — overview

The gl module is Priority 1 of the product: **versioned configuration and deterministic rule execution** ([spec 01_boundaries](../../../../acme.all.all.gl.spec/design/01_boundaries/specification.md)). It is built story by story:

| Story | Capability | State |
|---|---|---|
| US-003 | Tenant-scoped, versioned, effective-dated configuration store with audit | This module |
| US-004 | Roles and maker-checker on configuration changes | Next |
| US-005 … US-013 | Configuration sections: sources, events, measures, formulas, dimensions, dates, keys, COA, posting rules, account assignment | Planned |
| US-014 | Required-field manifest compiled from configuration | Planned |

Each configuration section registers its validator through `logic.RegisterSectionValidator`. The store, its versioning and its audit do not change when a section gains meaning.
