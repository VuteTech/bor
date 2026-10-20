// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

/**
 * Shared pieces of the SELinux and AppArmor editors: the node picker
 * (nodes that run the LSM, so the editor can show what one node does
 * now), the debounced catalogue search over what nodes reported plus the
 * server's curated lists, and the result list the editors add from.
 */

import React, { useEffect, useId, useMemo, useState } from "react";
import { Button, FormSelect, FormSelectOption, Label, SearchInput, Spinner, Tooltip } from "@patternfly/react-core";

import { LiveAlert } from "../../components/LiveAlert";
import { fetchNodesPaged, type Node } from "../../apiClient/nodesApi";
import { fetchMacCatalogue, type MacCatalogueItem, type MacItemKind } from "../../apiClient/macApi";

export const subtle: React.CSSProperties = { color: "var(--pf-t--global--text--color--subtle)" };

/** Nodes that run the given LSM, for the "show the state of" picker. */
export function useMacNodes(lsm: "selinux" | "apparmor"): { nodes: Node[]; error: string | null } {
  const [nodes, setNodes] = useState<Node[]>([]);
  const [error, setError] = useState<string | null>(null);
  useEffect(() => {
    let cancelled = false;
    fetchNodesPaged({ per_page: 200, sort_field: "name", sort_order: "asc", lsm })
      .then((res) => { if (!cancelled) setNodes(res.items.filter((n) => n.lsm === lsm)); })
      .catch((e: unknown) => { if (!cancelled) setError(e instanceof Error ? e.message : "Failed to load nodes"); });
    return () => { cancelled = true; };
  }, [lsm]);
  return { nodes, error };
}

/** Debounced catalogue search; a picked node narrows it to that node's items. */
export function useMacCatalogue(lsm: "selinux" | "apparmor", kind: MacItemKind, query: string, nodeId: string): { items: MacCatalogueItem[]; loading: boolean; error: string | null } {
  const [items, setItems] = useState<MacCatalogueItem[]>([]);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  useEffect(() => {
    let cancelled = false;
    setLoading(true);
    const handle = window.setTimeout(() => {
      fetchMacCatalogue({ lsm, kind, q: query.trim(), nodeId: nodeId || undefined, limit: 60 })
        .then((res) => { if (!cancelled) { setItems(res); setError(null); } })
        .catch((e: unknown) => { if (!cancelled) setError(e instanceof Error ? e.message : "Failed to load the catalogue"); })
        .finally(() => { if (!cancelled) setLoading(false); });
    }, query ? 250 : 0);
    return () => { cancelled = true; window.clearTimeout(handle); };
  }, [lsm, kind, query, nodeId]);
  return { items, loading, error };
}

interface NodePickerProps {
  lsm: "selinux" | "apparmor";
  value: string;
  onChange: (nodeId: string) => void;
  isDisabled?: boolean;
}

/** "Show the state of node ..." select. */
export const MacNodePicker: React.FC<NodePickerProps> = ({ lsm, value, onChange, isDisabled }) => {
  const id = useId();
  const { nodes, error } = useMacNodes(lsm);
  return (
    <div style={{ display: "flex", gap: "0.5rem", alignItems: "center", flexWrap: "wrap" }}>
      <label htmlFor={`${id}-node`} style={{ fontSize: "0.85rem" }}>Show the state of</label>
      <FormSelect id={`${id}-node`} value={value} onChange={(_ev, v) => onChange(v)} aria-label="Node whose state is shown" isDisabled={isDisabled} style={{ width: "16rem" }}>
        <FormSelectOption value="" label={nodes.length ? "every node (union)" : `no node runs ${lsm === "selinux" ? "SELinux" : "AppArmor"} yet`} />
        {nodes.map((n) => <FormSelectOption key={n.id} value={n.id} label={`${n.name}${n.lsm_mode ? ` (${n.lsm_mode})` : ""}`} />)}
      </FormSelect>
      <LiveAlert id={`${id}-node-error`} variant="danger" isInline message={error} />
    </div>
  );
};

interface CatalogueSearchProps {
  lsm: "selinux" | "apparmor";
  kind: MacItemKind;
  nodeId: string;
  /** Names already in the policy; shown but not addable twice. */
  taken: Set<string>;
  onPick: (item: MacCatalogueItem) => void;
  /** Secondary line of a result. */
  describe: (item: MacCatalogueItem) => string;
  placeholder: string;
  isDisabled?: boolean;
  /** Hide rows the editor cannot use (dynamic profiles, protected entries). */
  exclude?: (item: MacCatalogueItem) => string | null;
}

/** Search box plus a result list with an Add button per row. */
export const MacCatalogueSearch: React.FC<CatalogueSearchProps> = ({ lsm, kind, nodeId, taken, onPick, describe, placeholder, isDisabled, exclude }) => {
  const id = useId();
  const [query, setQuery] = useState("");
  const { items, loading, error } = useMacCatalogue(lsm, kind, query, nodeId);
  const shown = useMemo(() => items.slice(0, 40), [items]);
  return (
    <div>
      <SearchInput id={`${id}-search`} aria-label={placeholder} placeholder={placeholder} value={query} onChange={(_ev, v) => setQuery(v)} onClear={() => setQuery("")} isDisabled={isDisabled} />
      <LiveAlert id={`${id}-error`} variant="danger" isInline message={error} />
      {loading && <Spinner size="sm" aria-label="Loading" />}
      {!loading && shown.length === 0 && (
        <p style={{ ...subtle, fontSize: "0.85rem", marginTop: "0.4rem" }}>
          {query ? "Nothing matches." : "Nothing reported yet. Type a name to add it anyway."}
        </p>
      )}
      <ul style={{ listStyle: "none", margin: "0.4rem 0 0", padding: 0, maxHeight: "16rem", overflowY: "auto", border: "1px solid var(--pf-t--global--border--color--default)", borderRadius: "var(--pf-t--global--border--radius--small)" }} aria-label={`${placeholder} results`}>
        {shown.map((it) => {
          const blocked = exclude ? exclude(it) : null;
          const already = taken.has(it.name);
          return (
            <li key={`${it.kind}/${it.name}`} style={{ display: "flex", gap: "0.5rem", alignItems: "center", padding: "0.3rem 0.5rem", borderBottom: "1px solid var(--pf-t--global--border--color--default)" }}>
              <div style={{ minWidth: 0, flex: 1 }}>
                <div style={{ display: "flex", gap: "0.4rem", alignItems: "center", flexWrap: "wrap" }}>
                  <code style={{ fontSize: "0.85rem", wordBreak: "break-all" }}>{it.name}</code>
                  {it.group && <Label isCompact>{it.group}</Label>}
                  {it.curated && <Tooltip content="From the server's list; no node has reported it yet"><Label isCompact color="grey">curated</Label></Tooltip>}
                  {it.protected && <Label isCompact color="orange">protected</Label>}
                  {it.dynamic && <Tooltip content="Generated by other software on the node; cannot be managed"><Label isCompact color="grey">{it.info.managedBy}</Label></Tooltip>}
                  {it.nodeCount > 0 && !nodeId && <span style={{ ...subtle, fontSize: "0.75rem" }}>{it.nodeCount} node{it.nodeCount === 1 ? "" : "s"}</span>}
                </div>
                <div style={{ ...subtle, fontSize: "0.8rem" }}>{describe(it)}</div>
              </div>
              <Button variant="link" size="sm" isInline onClick={() => onPick(it)} isDisabled={isDisabled || already || blocked !== null} isAriaDisabled={already || blocked !== null} aria-label={`Add ${it.name}`}>
                {already ? "Added" : blocked ?? "Add"}
              </Button>
            </li>
          );
        })}
      </ul>
    </div>
  );
};
