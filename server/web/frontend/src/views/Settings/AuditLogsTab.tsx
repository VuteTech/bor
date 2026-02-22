// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

import React, { useState, useEffect, useCallback } from "react";
import {
  Alert,
  Button,
  Flex,
  FlexItem,
  Pagination,
  Spinner,
  TextInput,
  Toolbar,
  ToolbarContent,
  ToolbarItem,
} from "@patternfly/react-core";
import { Table, Thead, Tr, Th, Tbody, Td } from "@patternfly/react-table";
import DownloadIcon from "@patternfly/react-icons/dist/esm/icons/download-icon";

import { hasPermission } from "../../apiClient/permissions";
import {
  fetchAuditLogs,
  exportAuditLogs,
  AuditLog,
  AuditLogListParams,
} from "../../apiClient/auditLogsApi";

export const AuditLogsTab: React.FC = () => {
  const [logs, setLogs] = useState<AuditLog[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [page, setPage] = useState(1);
  const [perPage, setPerPage] = useState(25);
  const [total, setTotal] = useState(0);
  const [usernameFilter, setUsernameFilter] = useState("");
  const [exporting, setExporting] = useState(false);

  const canExport = hasPermission("audit_log:export");

  const reload = useCallback(() => {
    setLoading(true);
    setError(null);
    const params: AuditLogListParams = {
      page,
      per_page: perPage,
    };
    if (usernameFilter.trim()) {
      params.username = usernameFilter.trim();
    }
    fetchAuditLogs(params)
      .then((resp) => {
        setLogs(resp.items || []);
        setTotal(resp.total);
      })
      .catch((e) => setError(e.message))
      .finally(() => setLoading(false));
  }, [page, perPage, usernameFilter]);

  useEffect(() => {
    reload();
  }, [reload]);

  const handleExport = async (format: "csv" | "json") => {
    setExporting(true);
    setError(null);
    try {
      const params: AuditLogListParams = {};
      if (usernameFilter.trim()) {
        params.username = usernameFilter.trim();
      }
      await exportAuditLogs(format, params);
    } catch (e: unknown) {
      setError(e instanceof Error ? e.message : "Export failed");
    } finally {
      setExporting(false);
    }
  };

  const handleFilterKeyPress = (event: React.KeyboardEvent) => {
    if (event.key === "Enter") {
      setPage(1);
      reload();
    }
  };

  const formatTimestamp = (ts: string) => {
    try {
      return new Date(ts).toLocaleString();
    } catch {
      return ts;
    }
  };

  return (
    <>
      {error && (
        <Alert
          variant="danger"
          title={error}
          isInline
          style={{ marginBottom: 16 }}
        />
      )}

      <Toolbar>
        <ToolbarContent>
          <ToolbarItem>
            <TextInput
              type="text"
              aria-label="Filter by username"
              placeholder="Filter by username..."
              value={usernameFilter}
              onChange={(_ev, v) => setUsernameFilter(v)}
              onKeyUp={handleFilterKeyPress}
            />
          </ToolbarItem>
          {canExport && (
            <ToolbarItem align={{ default: "alignRight" }}>
              <Flex>
                <FlexItem>
                  <Button
                    variant="secondary"
                    icon={<DownloadIcon />}
                    onClick={() => handleExport("csv")}
                    isDisabled={exporting}
                    isLoading={exporting}
                  >
                    Export CSV
                  </Button>
                </FlexItem>
                <FlexItem>
                  <Button
                    variant="secondary"
                    icon={<DownloadIcon />}
                    onClick={() => handleExport("json")}
                    isDisabled={exporting}
                    isLoading={exporting}
                  >
                    Export JSON
                  </Button>
                </FlexItem>
              </Flex>
            </ToolbarItem>
          )}
        </ToolbarContent>
      </Toolbar>

      {loading ? (
        <Spinner size="lg" />
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
              {logs.map((entry) => (
                <Tr key={entry.id}>
                  <Td>{formatTimestamp(entry.created_at)}</Td>
                  <Td>{entry.username}</Td>
                  <Td>{entry.action}</Td>
                  <Td>{entry.resource_type}</Td>
                  <Td>
                    <span
                      style={{
                        maxWidth: 150,
                        display: "inline-block",
                        overflow: "hidden",
                        textOverflow: "ellipsis",
                        whiteSpace: "nowrap",
                      }}
                      title={entry.resource_id}
                    >
                      {entry.resource_id || "—"}
                    </span>
                  </Td>
                  <Td>
                    <span
                      style={{
                        maxWidth: 250,
                        display: "inline-block",
                        overflow: "hidden",
                        textOverflow: "ellipsis",
                        whiteSpace: "nowrap",
                      }}
                      title={entry.details}
                    >
                      {entry.details || "—"}
                    </span>
                  </Td>
                  <Td>{entry.ip_address || "—"}</Td>
                </Tr>
              ))}
              {logs.length === 0 && (
                <Tr>
                  <Td colSpan={7}>No audit log entries found.</Td>
                </Tr>
              )}
            </Tbody>
          </Table>

          <Pagination
            itemCount={total}
            page={page}
            perPage={perPage}
            onSetPage={(_ev, p) => setPage(p)}
            onPerPageSelect={(_ev, pp) => {
              setPerPage(pp);
              setPage(1);
            }}
            variant="bottom"
          />
        </>
      )}
    </>
  );
};
