# acme.all.all.gl.spec — GL Ecosystem specification

The design source of truth for the GL Ecosystem product. Code units (`acme.all.all.gl.app`, `acme.all.all.gl.wfc`) implement what is specified here; they do not redefine it.

| Path | Content |
|---|---|
| [design/00_overview.md](design/00_overview.md) | Purpose, end-to-end flow, design-time vs runtime, the units and modules |
| [design/01_boundaries/](design/01_boundaries/specification.md) | Responsibilities of each unit and module, what is in and out of scope, priorities |
| [design/02_canonical_contracts/](design/02_canonical_contracts/specification.md) | Canonical contracts (configuration version, field manifest, mapping version, financial event, source journal, journal entry, exception case) with JSON Schemas |
| [design/03_data_workbench_integration/](design/03_data_workbench_integration/specification.md) | How GL uses the existing Data Workbench APIs for discovery, mapping and collection |
| [design/adr/](design/adr/README.md) | Architecture decision records |
| [_harness/fixtures/](_harness/fixtures/GL-TEST-001.md) | GL-TEST-001, the synthetic Policy P1 acceptance scenario |
| [_harness/contracts/](_harness/contracts/README.md) | Executable walk-through of the premium, invoice and payment paths through the contracts |

Naming follows the reference structure (`<org>.all.all.<product>.{app,wfc,spec}`). The final product naming is provisional until the product owner confirms it.
