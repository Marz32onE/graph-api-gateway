## MODIFIED Requirements

### Requirement: Pure Merge Function
The codebase SHALL provide a pure `Merge(graphs ...*cytoscape.Body) *cytoscape.Body` function that takes any number of Cytoscape graphs (the shared kube-state-graph `pkg/cytoscape` DTO) and returns a single merged graph without mutating its inputs. The union-by-id and edge-dedup semantics are unchanged; only the value type changes from the gateway-local `*CytoscapeGraph` to the shared `*cytoscape.Body`.

#### Scenario: Inputs are not mutated
- **WHEN** `Merge(a, b)` is called
- **THEN** the contents of `a` and `b` remain byte-for-byte unchanged

#### Scenario: Duplicate ids resolve to first writer
- **WHEN** `a` contains node `{id:"x", type:"pod"}` and `b` contains node `{id:"x", type:"node"}`
- **THEN** the result contains one node `{id:"x", type:"pod"}`

#### Scenario: Duplicate edge triple collapses
- **WHEN** `a` and `b` both contain edge `{type:"t", source:"x", target:"y"}` (with possibly different `data.id`)
- **THEN** the result contains exactly one such edge, taken from `a`

### Requirement: IP Extraction Helper
The codebase SHALL provide a pure helper `ExtractIPs(*cytoscape.Body) []string` that walks every entry in `elements.nodes` whose `data.type` is `node` and collects every value from `data.ipaddress`. Pod / PVC / external entries are intentionally excluded — only K8s node IPs are forwarded to the switch backend. Returned IPs SHALL be deduplicated; insertion order SHALL be preserved on first occurrence. The function MUST NOT mutate its input. Only the parameter type changes (now the shared `*cytoscape.Body`); the collection semantics are unchanged.

#### Scenario: Collects IPs from node entries only
- **WHEN** the input contains a `node` entry with `ipaddress: ["10.0.0.1","10.0.0.2"]` and a `pod` entry with `ipaddress: ["10.1.0.5"]`
- **THEN** the result is `["10.0.0.1","10.0.0.2"]` (pod IP excluded)

#### Scenario: Deduplicates repeated IPs across multiple nodes
- **WHEN** two `node` entries both list `10.0.0.1` in their `ipaddress`
- **THEN** the result contains `10.0.0.1` exactly once

#### Scenario: Empty input returns empty slice
- **WHEN** the input is nil or contains no eligible entries
- **THEN** the result is a non-nil empty slice

### Requirement: Switch Reconciliation Helper
The codebase SHALL provide a pure helper `ReconcileSwitch(primary, switchGraph *cytoscape.Body) *cytoscape.Body` that re-anchors the switch graph onto the primary's K8s node IDs by matching `data.ipaddress`. The contract (IP index from primary `node`-type entries, shadow-ID → kube-ID rewrite map, drop collapsed shadows, rewrite remaining switch edge endpoints, non-mutating, `nil` switch passes through as `nil`) is unchanged; only the value type changes from `*CytoscapeGraph` to the shared `*cytoscape.Body`.

#### Scenario: Switch shadow node is collapsed onto kube node
- **WHEN** `primary` contains a node `{id:"prod/abc", type:"node", ipaddress:["10.0.0.1"]}`
- **AND** `switchGraph` contains a node `{id:"sw-host:xyz", type:"host", ipaddress:["10.0.0.1"]}` and an edge `{source:"sw-host:xyz", target:"switch:tor-1", type:"host-attached"}`
- **THEN** the returned graph contains no node with id `sw-host:xyz`
- **AND** the returned graph contains an edge `{source:"prod/abc", target:"switch:tor-1", type:"host-attached"}`

#### Scenario: Switch endpoint with unknown IP stays orphaned
- **WHEN** `switchGraph` contains a node `{id:"sw-host:foreign", type:"host", ipaddress:["172.31.99.99"]}` and `primary` has no node with that IP
- **THEN** the returned graph contains that node unchanged with its original ID, and any edges referencing it are unchanged

#### Scenario: Inputs are not mutated
- **WHEN** `ReconcileSwitch(primary, switchGraph)` is called
- **THEN** `primary` and `switchGraph` are byte-for-byte identical before and after

#### Scenario: Nil switch input passes through
- **WHEN** `switchGraph` is `nil`
- **THEN** the function returns `nil`
