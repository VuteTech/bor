// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

import React, { useState, useEffect, useCallback, useRef } from "react";
import {
  Alert,
  Button,
  DescriptionList,
  DescriptionListDescription,
  DescriptionListGroup,
  DescriptionListTerm,
  Flex,
  FlexItem,
  Menu,
  MenuContent,
  MenuItem,
  MenuList,
  MenuToggle,
  Modal,
  ModalVariant,
  PageSection,
  Pagination,
  Popper,
  Spinner,
  TextContent,
  TextInput,
  Toolbar,
  ToolbarContent,
  ToolbarGroup,
  ToolbarItem,
} from "@patternfly/react-core";
import { Table, Thead, Tr, Th, Tbody, Td } from "@patternfly/react-table";
import DownloadIcon from "@patternfly/react-icons/dist/esm/icons/download-icon";
import ExpandIcon from "@patternfly/react-icons/dist/esm/icons/expand-icon";
import SyncIcon from "@patternfly/react-icons/dist/esm/icons/sync-icon";
import TimesIcon from "@patternfly/react-icons/dist/esm/icons/times-icon";

import { hasPermission } from "../../apiClient/permissions";
import {
  fetchAuditLogs,
  exportAuditLogs,
  AuditLog,
  AuditLogListParams,
} from "../../apiClient/auditLogsApi";

// ─── Known filter values ──────────────────────────────────────────────────────

const KNOWN_ACTIONS = ["create", "update", "delete", "tamper_detected"];
const KNOWN_RESOURCE_TYPES = [
  "policies",
  "nodes",
  "node-groups",
  "users",
  "roles",
  "user-groups",
  "policy-bindings",
  "managed_file",
  "settings",
];

// ─── Color definitions ────────────────────────────────────────────────────────

interface ColorStyle { bg: string; color: string }

const ACTION_COLORS: Record<string, ColorStyle> = {
  create:           { bg: "#3e8635", color: "#fff" },
  update:           { bg: "#0066cc", color: "#fff" },
  delete:           { bg: "#c9190b", color: "#fff" },
  tamper_detected:  { bg: "#f0ab00", color: "#1f1f1f" },
};
const DEFAULT_ACTION_COLOR: ColorStyle = { bg: "#6a6e73", color: "#fff" };

const FILTER_TYPE_COLORS: Record<FilterType, ColorStyle> = {
  action:        { bg: "#6753ac", color: "#fff" },  // purple — overridden per-value below
  resource_type: { bg: "#009596", color: "#fff" },  // teal
  username:      { bg: "#4f5d75", color: "#fff" },  // slate
};

function chipColor(type: FilterType, value: string): ColorStyle {
  if (type === "action") return ACTION_COLORS[value] ?? DEFAULT_ACTION_COLOR;
  return FILTER_TYPE_COLORS[type];
}

// ─── Filter chip types ────────────────────────────────────────────────────────

type FilterType = "action" | "resource_type" | "username";

interface FilterChip {
  id: string;
  type: FilterType;
  value: string;
}

let _chipSeq = 0;
function nextChipId() { return String(++_chipSeq); }

// ─── Sub-components ───────────────────────────────────────────────────────────

const ActionBadge: React.FC<{ action: string }> = ({ action }) => {
  const { bg, color } = ACTION_COLORS[action] ?? DEFAULT_ACTION_COLOR;
  return (
    <span style={{
      background: bg, color, borderRadius: 9999,
      padding: "2px 10px", fontSize: "0.75rem", fontWeight: 600,
      whiteSpace: "nowrap", display: "inline-block",
    }}>
      {action}
    </span>
  );
};

const FilterPill: React.FC<{ chip: FilterChip; onRemove: () => void }> = ({ chip, onRemove }) => {
  const { bg, color } = chipColor(chip.type, chip.value);
  return (
    <span style={{
      display: "inline-flex", alignItems: "center", gap: 4,
      background: bg, color, borderRadius: 9999,
      padding: "2px 6px 2px 10px", fontSize: "0.8rem", fontWeight: 500,
    }}>
      {chip.value}
      <button
        aria-label={`Remove filter ${chip.value}`}
        onClick={onRemove}
        style={{
          background: "none", border: "none", cursor: "pointer",
          color: "inherit", padding: 0, display: "flex", alignItems: "center",
          opacity: 0.8, lineHeight: 1,
        }}
      >
        <TimesIcon style={{ fontSize: "0.7rem" }} />
      </button>
    </span>
  );
};

// Dropdown that adds chips for a fixed list of values.
const FilterDropdown: React.FC<{
  label: string;
  options: string[];
  activeValues: string[];
  onAdd: (value: string) => void;
}> = ({ label, options, activeValues, onAdd }) => {
  const [isOpen, setIsOpen] = useState(false);
  const toggleRef = useRef<HTMLButtonElement>(null);
  const menuRef = useRef<HTMLDivElement>(null);

  // Close on outside click
  useEffect(() => {
    if (!isOpen) return;
    const handler = (e: MouseEvent) => {
      if (
        toggleRef.current && !toggleRef.current.contains(e.target as Node) &&
        menuRef.current && !menuRef.current.contains(e.target as Node)
      ) {
        setIsOpen(false);
      }
    };
    document.addEventListener("mousedown", handler);
    return () => document.removeEventListener("mousedown", handler);
  }, [isOpen]);

  const available = options.filter((o) => !activeValues.includes(o));

  return (
    <Popper
      trigger={
        <MenuToggle
          ref={toggleRef}
          onClick={() => setIsOpen((v) => !v)}
          isExpanded={isOpen}
        >
          {label}
        </MenuToggle>
      }
      popper={
        <Menu ref={menuRef} onSelect={(_ev, itemId) => {
          if (typeof itemId === "string") {
            onAdd(itemId);
          }
        }}>
          <MenuContent>
            <MenuList>
              {available.length === 0
                ? <MenuItem isDisabled key="none">All values active</MenuItem>
                : available.map((opt) => (
                  <MenuItem key={opt} itemId={opt}>{opt}</MenuItem>
                ))
              }
            </MenuList>
          </MenuContent>
        </Menu>
      }
      isVisible={isOpen}
      enableFlip
    />
  );
};

// ─── Tamper event detail types ────────────────────────────────────────────────

interface TamperProcess { pid: number; comm: string; user: string }
interface TamperDetailsData { file?: string; node?: string; processes?: TamperProcess[] }

function parseTamperDetails(raw: string): TamperDetailsData | null {
  try {
    const d = JSON.parse(raw);
    if (d && typeof d === "object" && ("file" in d || "processes" in d)) return d;
  } catch { /* not JSON */ }
  return null;
}

function tamperDetailsSummary(d: TamperDetailsData): string {
  if (!d.processes || d.processes.length === 0) return "no process identified";
  const parts = d.processes.map((p) => `${p.comm} (${p.user}, pid ${p.pid})`);
  if (parts.length <= 2) return parts.join(", ");
  return `${parts[0]}, ${parts[1]} +${parts.length - 2} more`;
}

const TamperDetailsView: React.FC<{ data: TamperDetailsData }> = ({ data }) => (
  <div>
    <DescriptionList isCompact isHorizontal style={{ marginBottom: 16 }}>
      {data.file && (
        <DescriptionListGroup>
          <DescriptionListTerm>File</DescriptionListTerm>
          <DescriptionListDescription>
            <code style={{ wordBreak: "break-all" }}>{data.file}</code>
          </DescriptionListDescription>
        </DescriptionListGroup>
      )}
      {data.node && (
        <DescriptionListGroup>
          <DescriptionListTerm>Node</DescriptionListTerm>
          <DescriptionListDescription>{data.node}</DescriptionListDescription>
        </DescriptionListGroup>
      )}
    </DescriptionList>
    <strong>Processes with file open at detection time</strong>
    {!data.processes || data.processes.length === 0 ? (
      <p style={{ color: "var(--pf-v5-global--Color--200)", marginTop: 8 }}>
        No process identified — the modifying process had already exited.
      </p>
    ) : (
      <table style={{ width: "100%", marginTop: 8, borderCollapse: "collapse", fontSize: "var(--pf-v5-global--FontSize--sm)" }}>
        <thead>
          <tr>
            {["PID", "Process", "User"].map((h) => (
              <th key={h} style={{ textAlign: "left", padding: "4px 8px", borderBottom: "1px solid var(--pf-v5-global--BorderColor--100)", color: "var(--pf-v5-global--Color--200)", fontWeight: 600 }}>{h}</th>
            ))}
          </tr>
        </thead>
        <tbody>
          {data.processes.map((p) => (
            <tr key={p.pid}>
              <td style={{ padding: "4px 8px", fontFamily: "monospace" }}>{p.pid}</td>
              <td style={{ padding: "4px 8px", fontFamily: "monospace" }}>{p.comm}</td>
              <td style={{ padding: "4px 8px" }}>{p.user}</td>
            </tr>
          ))}
        </tbody>
      </table>
    )}
  </div>
);

// ─── Expand cell ──────────────────────────────────────────────────────────────

interface ExpandedField { label: string; content: string; tamperData?: TamperDetailsData }

const TruncatedCell: React.FC<{
  content: string; maxWidth: number; label: string;
  onExpand: (f: ExpandedField) => void; tamperData?: TamperDetailsData;
}> = ({ content, maxWidth, label, onExpand, tamperData }) => {
  if (!content) return <span>—</span>;
  return (
    <span style={{ display: "flex", alignItems: "center", gap: 4 }}>
      <span style={{ maxWidth, display: "inline-block", overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap", verticalAlign: "middle" }} title={content}>
        {content}
      </span>
      <Button variant="plain" aria-label={`Expand ${label}`}
        onClick={() => onExpand({ label, content, tamperData })}
        style={{ padding: 0, minWidth: "auto", height: "auto", lineHeight: 1 }}
      >
        <ExpandIcon style={{ fontSize: "0.75rem", color: "var(--pf-v5-global--Color--200)" }} />
      </Button>
    </span>
  );
};

const formatUser = (entry: AuditLog) =>
  entry.action === "tamper_detected" ? `node: ${entry.username}` : entry.username;

const formatTimestamp = (ts: string) => {
  try { return new Date(ts).toLocaleString(); } catch { return ts; }
};

// ─── Main page ────────────────────────────────────────────────────────────────

export const AuditLogsPage: React.FC = () => {
  const [logs, setLogs] = useState<AuditLog[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [page, setPage] = useState(1);
  const [perPage, setPerPage] = useState(25);
  const [total, setTotal] = useState(0);
  const [filters, setFilters] = useState<FilterChip[]>([]);
  const [usernameInput, setUsernameInput] = useState("");
  const [exporting, setExporting] = useState(false);
  const [expandedField, setExpandedField] = useState<ExpandedField | null>(null);

  const canExport = hasPermission("audit_log:export");

  const activeActions = filters.filter((f) => f.type === "action").map((f) => f.value);
  const activeResourceTypes = filters.filter((f) => f.type === "resource_type").map((f) => f.value);
  const activeUsernames = filters.filter((f) => f.type === "username").map((f) => f.value);

  const addFilter = (type: FilterType, value: string) => {
    if (filters.some((f) => f.type === type && f.value === value)) return;
    setFilters((prev) => [...prev, { id: nextChipId(), type, value }]);
    setPage(1);
  };

  const removeFilter = (id: string) => {
    setFilters((prev) => prev.filter((f) => f.id !== id));
    setPage(1);
  };

  const clearAllFilters = () => { setFilters([]); setPage(1); };

  const commitUsername = () => {
    const v = usernameInput.trim();
    if (v) { addFilter("username", v); setUsernameInput(""); }
  };

  const reload = useCallback(() => {
    setLoading(true);
    setError(null);

    const params: AuditLogListParams = { page, per_page: perPage };
    if (activeActions.length > 0) params.action = activeActions;
    if (activeResourceTypes.length > 0) params.resource_type = activeResourceTypes;
    // Send first username only (server supports single fuzzy match)
    if (activeUsernames.length > 0) params.username = activeUsernames[0];

    fetchAuditLogs(params)
      .then((resp) => { setLogs(resp.items || []); setTotal(resp.total); })
      .catch((e) => setError(e.message))
      .finally(() => setLoading(false));
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [page, perPage, filters]);

  useEffect(() => { reload(); }, [reload]);

  const handleExport = async (format: "csv" | "json") => {
    setExporting(true);
    setError(null);
    try {
      const params: AuditLogListParams = {};
      if (activeActions.length > 0) params.action = activeActions;
      if (activeResourceTypes.length > 0) params.resource_type = activeResourceTypes;
      if (activeUsernames.length > 0) params.username = activeUsernames[0];
      await exportAuditLogs(format, params);
    } catch (e: unknown) {
      setError(e instanceof Error ? e.message : "Export failed");
    } finally {
      setExporting(false);
    }
  };

  return (
    <PageSection>
      {error && <Alert variant="danger" title={error} isInline style={{ marginBottom: 16 }} />}

      {/* ── Toolbar ── */}
      <Toolbar clearAllFilters={filters.length > 0 ? clearAllFilters : undefined}>
        <ToolbarContent>
          {/* Action filter */}
          <ToolbarItem>
            <FilterDropdown
              label="Action"
              options={KNOWN_ACTIONS}
              activeValues={activeActions}
              onAdd={(v) => addFilter("action", v)}
            />
          </ToolbarItem>

          {/* Resource Type filter */}
          <ToolbarItem>
            <FilterDropdown
              label="Resource Type"
              options={KNOWN_RESOURCE_TYPES}
              activeValues={activeResourceTypes}
              onAdd={(v) => addFilter("resource_type", v)}
            />
          </ToolbarItem>

          {/* Username free-text filter */}
          <ToolbarItem>
            <Flex spaceItems={{ default: "spaceItemsSm" }}>
              <FlexItem>
                <TextInput
                  type="text"
                  aria-label="Filter by username"
                  placeholder="Username…"
                  value={usernameInput}
                  onChange={(_ev, v) => setUsernameInput(v)}
                  onKeyUp={(e) => { if (e.key === "Enter") commitUsername(); }}
                  style={{ width: 160 }}
                />
              </FlexItem>
              <FlexItem>
                <Button variant="control" onClick={commitUsername}>Add</Button>
              </FlexItem>
            </Flex>
          </ToolbarItem>

          {/* Refresh */}
          <ToolbarItem>
            <Button variant="plain" aria-label="Refresh" onClick={reload} isDisabled={loading}>
              <SyncIcon />
            </Button>
          </ToolbarItem>

          {/* Export buttons */}
          {canExport && (
            <ToolbarGroup align={{ default: "alignRight" }}>
              <ToolbarItem>
                <Button variant="secondary" icon={<DownloadIcon />} onClick={() => handleExport("csv")} isDisabled={exporting} isLoading={exporting}>
                  Export CSV
                </Button>
              </ToolbarItem>
              <ToolbarItem>
                <Button variant="secondary" icon={<DownloadIcon />} onClick={() => handleExport("json")} isDisabled={exporting} isLoading={exporting}>
                  Export JSON
                </Button>
              </ToolbarItem>
            </ToolbarGroup>
          )}
        </ToolbarContent>

        {/* ── Active filter chips ── */}
        {filters.length > 0 && (
          <ToolbarContent>
            <ToolbarItem>
              <Flex spaceItems={{ default: "spaceItemsSm" }} flexWrap={{ default: "wrap" }}>
                {filters.map((chip) => (
                  <FlexItem key={chip.id}>
                    <FilterPill chip={chip} onRemove={() => removeFilter(chip.id)} />
                  </FlexItem>
                ))}
                <FlexItem>
                  <Button variant="link" isInline onClick={clearAllFilters} style={{ fontSize: "0.8rem" }}>
                    Clear all
                  </Button>
                </FlexItem>
              </Flex>
            </ToolbarItem>
          </ToolbarContent>
        )}
      </Toolbar>

      {/* ── Table ── */}
      {loading ? (
        <Spinner size="lg" style={{ marginTop: 32 }} />
      ) : (
        <>
          <Table aria-label="Audit logs table" variant="compact">
            <Thead>
              <Tr>
                <Th>Timestamp</Th>
                <Th>User</Th>
                <Th>Action</Th>
                <Th>Resource Type</Th>
                <Th>Resource ID</Th>
                <Th>Details</Th>
                <Th>IP Address</Th>
              </Tr>
            </Thead>
            <Tbody>
              {logs.map((entry) => {
                const tamperData =
                  entry.action === "tamper_detected" ? parseTamperDetails(entry.details) : null;
                const detailsDisplay = tamperData
                  ? tamperDetailsSummary(tamperData)
                  : entry.details;

                return (
                  <Tr key={entry.id}>
                    <Td style={{ whiteSpace: "nowrap" }}>{formatTimestamp(entry.created_at)}</Td>
                    <Td>
                      <TruncatedCell content={formatUser(entry)} maxWidth={160} label="User" onExpand={setExpandedField} />
                    </Td>
                    <Td>
                      <ActionBadge action={entry.action} />
                    </Td>
                    <Td>
                      <TruncatedCell content={entry.resource_type} maxWidth={120} label="Resource Type" onExpand={setExpandedField} />
                    </Td>
                    <Td>
                      <TruncatedCell content={entry.resource_id} maxWidth={150} label="Resource ID" onExpand={setExpandedField} />
                    </Td>
                    <Td>
                      <TruncatedCell content={detailsDisplay} maxWidth={250} label="Details" onExpand={setExpandedField} tamperData={tamperData ?? undefined} />
                    </Td>
                    <Td>
                      <TruncatedCell content={entry.ip_address} maxWidth={140} label="IP Address" onExpand={setExpandedField} />
                    </Td>
                  </Tr>
                );
              })}
              {logs.length === 0 && (
                <Tr><Td colSpan={7}>No audit log entries found.</Td></Tr>
              )}
            </Tbody>
          </Table>

          <Pagination
            itemCount={total}
            page={page}
            perPage={perPage}
            onSetPage={(_ev, p) => setPage(p)}
            onPerPageSelect={(_ev, pp) => { setPerPage(pp); setPage(1); }}
            variant="bottom"
          />
        </>
      )}

      {/* ── Expand modal ── */}
      <Modal
        variant={ModalVariant.small}
        title={expandedField?.label ?? ""}
        isOpen={expandedField !== null}
        onClose={() => setExpandedField(null)}
        actions={[
          <Button key="close" variant="primary" onClick={() => setExpandedField(null)}>Close</Button>,
        ]}
      >
        {expandedField?.tamperData ? (
          <TamperDetailsView data={expandedField.tamperData} />
        ) : (
          <TextContent>
            <pre style={{ whiteSpace: "pre-wrap", wordBreak: "break-all", margin: 0 }}>
              {expandedField?.content}
            </pre>
          </TextContent>
        )}
      </Modal>
    </PageSection>
  );
};
