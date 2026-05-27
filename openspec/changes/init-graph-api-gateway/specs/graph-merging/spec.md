## ADDED Requirements

### Requirement: Pure Merge Function
The codebase SHALL provide a pure `Merge(graphs ...*CytoscapeGraph) *CytoscapeGraph` function that takes any number of Cytoscape graphs and returns a single merged graph without mutating its inputs.

#### Scenario: Inputs are not mutated
- **WHEN** `Merge(a, b)` is called
- **THEN** the contents of `a` and `b` remain byte-for-byte unchanged

### Requirement: Union by Node ID
Merged nodes SHALL be the union of input nodes keyed by `data.id`; on collision, the first occurrence wins.

#### Scenario: Distinct ids are both included
- **WHEN** `a` contains node id `x` and `b` contains node id `y`
- **THEN** the result contains exactly two nodes with ids `x` and `y`

#### Scenario: Duplicate ids resolve to first writer
- **WHEN** `a` contains node `{id:"x", type:"pod"}` and `b` contains node `{id:"x", type:"node"}`
- **THEN** the result contains one node `{id:"x", type:"pod"}`

### Requirement: Edge Deduplication
Merged edges SHALL be deduplicated by the triple `(type, source, target)`; the first occurrence wins.

#### Scenario: Distinct triples are both kept
- **WHEN** `a` contains edge `{type:"t", source:"x", target:"y"}` and `b` contains edge `{type:"t", source:"y", target:"z"}`
- **THEN** the result contains both edges

#### Scenario: Duplicate triple collapses
- **WHEN** `a` and `b` both contain edge `{type:"t", source:"x", target:"y"}` (with possibly different `data.id`)
- **THEN** the result contains exactly one such edge, taken from `a`

### Requirement: Envelope Preservation
The merged graph SHALL carry `apiVersion: "v1"` and SHALL place results under `elements.nodes` and `elements.edges`.

#### Scenario: Result envelope shape
- **WHEN** `Merge` is called with any inputs
- **THEN** the returned value serialises to `{"apiVersion":"v1","elements":{"nodes":[...],"edges":[...]}}`

### Requirement: IP Extraction Helper
The codebase SHALL provide a pure helper `ExtractIPs(*CytoscapeGraph) []string` that walks every entry in `elements.nodes` whose `data.type` is `node` and collects every value from `data.ipaddress`. Pod / PVC / external entries are intentionally excluded — only K8s node IPs are forwarded to the switch backend. Returned IPs SHALL be deduplicated; insertion order SHALL be preserved on first occurrence. The function MUST NOT mutate its input.

#### Scenario: Collects IPs from node entries only
- **WHEN** the input contains a `node` entry with `ipaddress: ["10.0.0.1","10.0.0.2"]` and a `pod` entry with `ipaddress: ["10.1.0.5"]`
- **THEN** the result is `["10.0.0.1","10.0.0.2"]` (pod IP excluded)

#### Scenario: Deduplicates repeated IPs across multiple nodes
- **WHEN** two `node` entries both list `10.0.0.1` in their `ipaddress`
- **THEN** the result contains `10.0.0.1` exactly once

#### Scenario: Skips entries without ipaddress
- **WHEN** a `node` entry's `ipaddress` is `nil`, `[]`, or missing
- **THEN** the result does not include any value from that entry

#### Scenario: Ignores non-node types
- **WHEN** the input contains entries whose `data.type` is `pod`, `pvc`, or `external` with non-empty `ipaddress`
- **THEN** the result does not include those IPs

#### Scenario: Empty input returns empty slice
- **WHEN** the input is nil or contains no eligible entries
- **THEN** the result is a non-nil empty slice

### Requirement: Switch Reconciliation Helper
The codebase SHALL provide a pure helper `ReconcileSwitch(primary, switchGraph *CytoscapeGraph) *CytoscapeGraph` that re-anchors the switch graph onto the primary's K8s node IDs by matching `data.ipaddress`. The contract:

1. Build an index `ipToNodeID` from every `node`-type entry in `primary` (one entry per IP value in its `data.ipaddress`).
2. Build a rewrite map `switchID → nodeID` by walking `switchGraph.elements.nodes`: for each switch node whose `data.ipaddress` contains an IP in `ipToNodeID`, record the mapping (first IP match wins per switch node).
3. Return a new `*CytoscapeGraph` where: (a) any switch node whose ID appears in the rewrite map is **dropped** (the kube node already represents it); (b) all remaining switch nodes are kept as-is; (c) every switch edge has its `source` and `target` rewritten through the map when present.

The function MUST NOT mutate either input. When `switchGraph` is `nil`, it SHALL return `nil` (downstream `Merge` already skips nil inputs).

#### Scenario: Switch shadow node is collapsed onto kube node
- **WHEN** `primary` contains a node `{id:"prod/abc", type:"node", ipaddress:["10.0.0.1"]}`
- **AND** `switchGraph` contains a node `{id:"sw-host:xyz", type:"host", ipaddress:["10.0.0.1"]}` and an edge `{source:"sw-host:xyz", target:"switch:tor-1", type:"host-attached"}`
- **THEN** the returned graph contains no node with id `sw-host:xyz`
- **AND** the returned graph contains an edge `{source:"prod/abc", target:"switch:tor-1", type:"host-attached"}`

#### Scenario: Switch-only nodes are preserved
- **WHEN** `switchGraph` contains a node `{id:"switch:tor-1", type:"switch"}` with no `ipaddress` (or an `ipaddress` that does not match any kube node)
- **THEN** the returned graph contains that node unchanged

#### Scenario: Switch endpoint with unknown IP stays orphaned
- **WHEN** `switchGraph` contains a node `{id:"sw-host:foreign", type:"host", ipaddress:["172.31.99.99"]}` and `primary` has no node with that IP
- **THEN** the returned graph contains that node unchanged with its original ID, and any edges referencing it are unchanged

#### Scenario: Multiple IPs on one kube node all collapse the same switch shadow
- **WHEN** `primary` contains `{id:"prod/abc", type:"node", ipaddress:["10.0.0.1","10.0.0.2"]}`
- **AND** `switchGraph` contains `{id:"sw-host:xyz", ipaddress:["10.0.0.2"]}` (matching the second IP)
- **THEN** that switch node is collapsed onto `prod/abc`

#### Scenario: Inputs are not mutated
- **WHEN** `ReconcileSwitch(primary, switchGraph)` is called
- **THEN** `primary` and `switchGraph` are byte-for-byte identical before and after

#### Scenario: Nil switch input passes through
- **WHEN** `switchGraph` is `nil`
- **THEN** the function returns `nil`
