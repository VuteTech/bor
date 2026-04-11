// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

import React, { useState, useEffect, useCallback, useMemo } from "react";
import {
  PageSection,
  Title,
  Spinner,
  Flex,
  FlexItem,
  Button,
  Label,
  Toolbar,
  ToolbarContent,
  ToolbarItem,
  SearchInput,
  MenuToggle,
  MenuToggleElement,
  Select,
  SelectOption,
  SelectList,
} from "@patternfly/react-core";
import { Table, Thead, Tr, Th, Tbody, Td, ExpandableRowContent } from "@patternfly/react-table";
import SyncAltIcon from "@patternfly/react-icons/dist/esm/icons/sync-alt-icon";

import { LiveAlert } from "../../components/LiveAlert";
import {
  fetchComplianceResults,
  fetchDConfSchemas,
  ComplianceResult,
  ComplianceStatus,
  ComplianceItem,
  DConfSchema,
} from "../../apiClient/dconfApi";

/* ── helpers ── */

const STATUS_LABELS: Record<ComplianceStatus, string> = {
  unknown:        "Unknown",
  compliant:      "Compliant",
  non_compliant:  "Non-Compliant",
  inapplicable:   "Inapplicable",
  error:          "Error",
};

const STATUS_COLORS: Record<ComplianceStatus, "green" | "red" | "grey" | "yellow" | "orange"> = {
  unknown:        "grey",
  compliant:      "green",
  non_compliant:  "red",
  inapplicable:   "grey",
  error:          "yellow",
};

const ALL_STATUSES: ComplianceStatus[] = ["unknown", "compliant", "non_compliant", "inapplicable", "error"];

function formatDate(raw: string): string {
  if (!raw) return "—";
  try {
    return new Date(raw).toLocaleString();
  } catch {
    return raw;
  }
}

/** Build a lookup: "${schema_id}/${key}" → summary string. */
function buildSummaryIndex(schemas: DConfSchema[]): Map<string, string> {
  const idx = new Map<string, string>();
  for (const schema of schemas) {
    for (const key of schema.keys ?? []) {
      if (key.summary) {
        idx.set(`${schema.schema_id}/${key.name}`, key.summary);
      }
    }
  }
  return idx;
}

/* ── per-item breakdown sub-table ── */

interface ItemsTableProps {
  items: ComplianceItem[];
  summaryIndex: Map<string, string>;
}

const ItemsTable: React.FC<ItemsTableProps> = ({ items, summaryIndex }) => (
  <Table aria-label="Per-key compliance breakdown" variant="compact" borders={false}
    style={{ marginLeft: "2rem", marginTop: "0.5rem", marginBottom: "0.5rem" }}
  >
    <Thead>
      <Tr>
        <Th>Key</Th>
        <Th>Status</Th>
        <Th>Details</Th>
      </Tr>
    </Thead>
    <Tbody>
      {items.map((item, i) => {
        const label = summaryIndex.get(`${item.schema_id}/${item.key}`) ?? item.key;
        const subtitle = summaryIndex.has(`${item.schema_id}/${item.key}`)
          ? `${item.schema_id} / ${item.key}`
          : item.schema_id;
        return (
          <Tr key={i}>
            <Td dataLabel="Key">
              <span>{label}</span>
              <br />
              <small style={{ color: "var(--pf-t--global--text--color--subtle)", fontFamily: "monospace" }}>
                {subtitle}
              </small>
            </Td>
            <Td dataLabel="Status">
              <Label color={STATUS_COLORS[item.status] ?? "grey"} isCompact>
                {STATUS_LABELS[item.status] ?? item.status}
              </Label>
            </Td>
            <Td dataLabel="Details">{item.message ?? "—"}</Td>
          </Tr>
        );
      })}
    </Tbody>
  </Table>
);

/* ── component ── */

export const CompliancePage: React.FC = () => {
  const [results, setResults] = useState<ComplianceResult[]>([]);
  const [schemas, setSchemas] = useState<DConfSchema[]>([]);
  const [loading, setLoading] = useState(true);
  const [refreshing, setRefreshing] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [expandedRows, setExpandedRows] = useState<Set<string>>(new Set());

  // Filters
  const [searchText, setSearchText] = useState("");
  const [statusFilter, setStatusFilter] = useState<string>("All");
  const [statusOpen, setStatusOpen] = useState(false);

  const load = useCallback(async (silent = false) => {
    try {
      if (!silent) setLoading(true);
      else setRefreshing(true);
      setError(null);
      const [data, schemaData] = await Promise.all([
        fetchComplianceResults(),
        fetchDConfSchemas().catch(() => [] as DConfSchema[]),
      ]);
      setResults(data);
      setSchemas(schemaData);
    } catch (err) {
      setError(err instanceof Error ? err.message : "Failed to load compliance results");
    } finally {
      setLoading(false);
      setRefreshing(false);
    }
  }, []);

  useEffect(() => { load(); }, [load]);

  const summaryIndex = useMemo(() => buildSummaryIndex(schemas), [schemas]);

  const filtered = useMemo(() => {
    const search = searchText.toLowerCase();
    return results.filter(r => {
      if (statusFilter !== "All" && r.status !== statusFilter) return false;
      if (search && !r.node_name.toLowerCase().includes(search) && !r.policy_name.toLowerCase().includes(search)) return false;
      return true;
    });
  }, [results, searchText, statusFilter]);

  /* ── status summary counts ── */
  const counts = useMemo(() => {
    const c: Record<string, number> = {};
    for (const r of results) {
      c[r.status] = (c[r.status] ?? 0) + 1;
    }
    return c;
  }, [results]);

  const rowKey = (r: ComplianceResult) => `${r.node_id}-${r.policy_id}`;

  const toggleRow = (key: string) => {
    setExpandedRows(prev => {
      const next = new Set(prev);
      if (next.has(key)) next.delete(key);
      else next.add(key);
      return next;
    });
  };

  if (loading) {
    return (
      <PageSection>
        <Flex justifyContent={{ default: "justifyContentCenter" }}>
          <FlexItem>
            <Spinner size="xl" aria-label="Loading compliance results" />
          </FlexItem>
        </Flex>
      </PageSection>
    );
  }

  return (
    <PageSection>
      <Flex justifyContent={{ default: "justifyContentSpaceBetween" }} alignItems={{ default: "alignItemsCenter" }} style={{ marginBottom: "1rem" }}>
        <FlexItem>
          <Title headingLevel="h1" size="xl">Compliance</Title>
        </FlexItem>
        <FlexItem>
          <Button
            variant="plain"
            onClick={() => load(true)}
            isLoading={refreshing}
            isDisabled={refreshing}
            aria-label="Refresh compliance results"
          >
            <SyncAltIcon />
          </Button>
        </FlexItem>
      </Flex>

      <LiveAlert message={error} variant="danger" style={{ marginBottom: "1rem" }} />

      {/* Summary chips */}
      <Flex spaceItems={{ default: "spaceItemsSm" }} style={{ marginBottom: "1.25rem" }}>
        {ALL_STATUSES.map(s => (
          counts[s] !== undefined
            ? (
              <FlexItem key={s}>
                <Label color={STATUS_COLORS[s]} isCompact>
                  {STATUS_LABELS[s]}: {counts[s]}
                </Label>
              </FlexItem>
            ) : null
        ))}
      </Flex>

      {/* Toolbar */}
      <Toolbar clearAllFilters={() => { setSearchText(""); setStatusFilter("All"); }}>
        <ToolbarContent>
          <ToolbarItem>
            <SearchInput
              placeholder="Search node or policy…"
              value={searchText}
              onChange={(_ev, val) => setSearchText(val)}
              onClear={() => setSearchText("")}
              aria-label="Search compliance results"
            />
          </ToolbarItem>
          <ToolbarItem>
            <Select
              id="compliance-status-filter"
              isOpen={statusOpen}
              onOpenChange={setStatusOpen}
              selected={statusFilter}
              onSelect={(_ev, val) => { setStatusFilter(val as string); setStatusOpen(false); }}
              toggle={(ref: React.Ref<MenuToggleElement>) => (
                <MenuToggle
                  ref={ref}
                  onClick={() => setStatusOpen(v => !v)}
                  isExpanded={statusOpen}
                  aria-label="Filter by compliance status"
                >
                  Status: {statusFilter}
                </MenuToggle>
              )}
            >
              <SelectList>
                <SelectOption value="All">All</SelectOption>
                {ALL_STATUSES.map(s => (
                  <SelectOption key={s} value={s}>{STATUS_LABELS[s]}</SelectOption>
                ))}
              </SelectList>
            </Select>
          </ToolbarItem>
        </ToolbarContent>
      </Toolbar>

      {filtered.length === 0 ? (
        <div style={{ padding: "2rem", textAlign: "center", color: "var(--pf-t--global--text--color--subtle)" }}>
          {results.length === 0
            ? "No compliance data yet. Agents will report status when they apply policies."
            : "No results match the current filters."}
        </div>
      ) : (
        <Table aria-label="Compliance results" variant="compact">
          <Thead>
            <Tr>
              {/* expand-toggle column — always present so column counts stay consistent */}
              <Th screenReaderText="Row expand" />
              <Th>Node</Th>
              <Th>Policy</Th>
              <Th>Status</Th>
              <Th>Message</Th>
              <Th>Reported</Th>
            </Tr>
          </Thead>
          {filtered.map((r, idx) => {
            const key = rowKey(r);
            const isExpanded = expandedRows.has(key);
            const hasItems = (r.items?.length ?? 0) > 0;

            return (
              <Tbody key={`${key}-${idx}`} isExpanded={hasItems ? isExpanded : undefined}>
                <Tr>
                  {hasItems ? (
                    <Td
                      expand={{
                        rowIndex: idx,
                        isExpanded,
                        onToggle: () => toggleRow(key),
                        expandId: `expand-toggle-${key}`,
                      }}
                    />
                  ) : (
                    /* Keep a placeholder cell so the column count stays at 6 for rows
                       that have no expandable content (non-dconf or pre-items data). */
                    <Td className="pf-v6-c-table__toggle" />
                  )}
                  <Td dataLabel="Node">{r.node_name}</Td>
                  <Td dataLabel="Policy">{r.policy_name}</Td>
                  <Td dataLabel="Status">
                    <Label color={STATUS_COLORS[r.status]} isCompact>
                      {STATUS_LABELS[r.status] ?? r.status}
                    </Label>
                  </Td>
                  <Td dataLabel="Message">{r.message ?? "—"}</Td>
                  <Td dataLabel="Reported">{formatDate(r.reported_at)}</Td>
                </Tr>
                {hasItems && (
                  <Tr isExpanded={isExpanded}>
                    <Td colSpan={6} noPadding>
                      <ExpandableRowContent>
                        <ItemsTable items={r.items!} summaryIndex={summaryIndex} />
                      </ExpandableRowContent>
                    </Td>
                  </Tr>
                )}
              </Tbody>
            );
          })}
        </Table>
      )}
    </PageSection>
  );
};
