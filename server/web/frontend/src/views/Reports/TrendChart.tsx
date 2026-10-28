// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

/**
 * TrendChart: the compliance score over time from the daily summaries. One
 * series, so no legend box (the card title names it). A 2px line with a light
 * area wash, a labelled end point, a crosshair that snaps to the nearest day
 * on pointer and keyboard, and a table view for screen readers and print.
 */

import React, { useEffect, useId, useMemo, useRef, useState } from "react";
import { Button } from "@patternfly/react-core";
import { Table, Thead, Tr, Th, Tbody, Td } from "@patternfly/react-table";

import { ReportTrendPoint } from "../../apiClient/reportsApi";

interface TrendChartProps {
  points: ReportTrendPoint[];
  /** Plot height in px (default 160). */
  height?: number;
  /** Sparkline: no axes, no table toggle. */
  compact?: boolean;
  /** Accessible name of the chart. */
  title: string;
}

const LINE = "var(--pf-t--global--color--brand--default)";
const GRID = "var(--pf-t--global--border--color--default)";
const MUTED = "var(--pf-t--global--text--color--subtle)";
const SURFACE = "var(--pf-t--global--background--color--primary--default)";

function day(iso: string): string {
  return new Date(iso).toLocaleDateString(undefined, { month: "short", day: "numeric" });
}

function pct(v: number | null): string {
  return v === null ? "n/a" : `${v.toFixed(1)}%`;
}

export const TrendChart: React.FC<TrendChartProps> = ({ points, height = 160, compact = false, title }) => {
  const id = useId();
  const [active, setActive] = useState<number | null>(null);
  const [showTable, setShowTable] = useState(false);

  const boxRef = useRef<HTMLDivElement>(null);
  const [width, setWidth] = useState(600);
  useEffect(() => {
    const el = boxRef.current;
    if (!el || typeof ResizeObserver === "undefined") return;
    const ro = new ResizeObserver((entries) => {
      const w = Math.round(entries[0].contentRect.width);
      if (w > 0) setWidth(w);
    });
    ro.observe(el);
    return () => ro.disconnect();
  }, []);
  const pad = compact ? { top: 6, right: 6, bottom: 6, left: 6 } : { top: 12, right: 52, bottom: 24, left: 40 };
  const plotW = width - pad.left - pad.right;
  const plotH = height - pad.top - pad.bottom;

  const scored = useMemo(() => points.map((p, i) => ({ ...p, i })).filter((p) => p.score_pct !== null), [points]);
  // The y axis spans 0-100 unless every value is high, then it zooms to the
  // band the data lives in (rounded to tens) so changes stay visible.
  const minScore = scored.length ? Math.min(...scored.map((p) => p.score_pct as number)) : 0;
  const yMin = minScore >= 60 ? Math.floor((minScore - 5) / 10) * 10 : 0;
  // Days are placed by time, so a missing daily summary leaves a gap in
  // the spacing rather than compressing the axis.
  const times = useMemo(() => points.map((p) => new Date(p.generated_at).getTime()), [points]);
  const span = times.length > 1 ? times[times.length - 1] - times[0] : 0;
  const x = (i: number) => pad.left + (span <= 0 ? plotW / 2 : ((times[i] - times[0]) / span) * plotW);
  const y = (v: number) => pad.top + plotH - ((v - yMin) / (100 - yMin)) * plotH;

  const linePath = scored.map((p, k) => `${k === 0 ? "M" : "L"}${x(p.i).toFixed(1)},${y(p.score_pct as number).toFixed(1)}`).join(" ");
  const areaPath = scored.length > 1
    ? `${linePath} L${x(scored[scored.length - 1].i).toFixed(1)},${(pad.top + plotH).toFixed(1)} L${x(scored[0].i).toFixed(1)},${(pad.top + plotH).toFixed(1)} Z`
    : "";
  const last = scored.length ? scored[scored.length - 1] : null;

  const nearest = (clientX: number, rect: DOMRect): number => {
    const sx = ((clientX - rect.left) / rect.width) * width;
    let best = 0;
    for (let i = 1; i < points.length; i++) {
      if (Math.abs(x(i) - sx) < Math.abs(x(best) - sx)) best = i;
    }
    return best;
  };

  const onKey = (e: React.KeyboardEvent) => {
    if (points.length === 0) return;
    if (e.key === "ArrowRight" || e.key === "ArrowLeft") {
      e.preventDefault();
      const cur = active ?? points.length - 1;
      setActive(Math.max(0, Math.min(points.length - 1, cur + (e.key === "ArrowRight" ? 1 : -1))));
    } else if (e.key === "Escape") {
      setActive(null);
    }
  };

  const activePoint = active !== null ? points[active] : null;
  const ticks = yMin === 0 ? [0, 50, 100] : [yMin, Math.round((yMin + 100) / 2), 100];

  return (
    <div>
      <div ref={boxRef} style={{ position: "relative" }}>
        <svg
          viewBox={`0 0 ${width} ${height}`}
          width={width}
          height={height}
          role="img"
          aria-label={`${title}: ${points.length} days${last ? `, latest ${pct(last.score_pct)}` : ""}`}
          tabIndex={compact ? -1 : 0}
          onPointerMove={(e) => points.length && setActive(nearest(e.clientX, e.currentTarget.getBoundingClientRect()))}
          onPointerLeave={() => setActive(null)}
          onKeyDown={onKey}
          onBlur={() => setActive(null)}
          style={{ display: "block", overflow: "visible", touchAction: "none" }}
        >
          {!compact && ticks.map((t) => (
            <g key={t}>
              <line x1={pad.left} x2={pad.left + plotW} y1={y(t)} y2={y(t)} stroke={GRID} strokeWidth={1} />
              <text x={pad.left - 6} y={y(t) + 4} textAnchor="end" fontSize={11} fill={MUTED}>{t}%</text>
            </g>
          ))}
          {!compact && points.length > 0 && (
            <>
              <text x={x(0)} y={height - 6} textAnchor="start" fontSize={11} fill={MUTED}>{day(points[0].generated_at)}</text>
              {points.length > 1 && (
                <text x={x(points.length - 1)} y={height - 6} textAnchor="end" fontSize={11} fill={MUTED}>{day(points[points.length - 1].generated_at)}</text>
              )}
            </>
          )}
          {areaPath && <path d={areaPath} fill={LINE} fillOpacity={0.1} stroke="none" />}
          {linePath && (
            <path d={linePath} fill="none" stroke={LINE} strokeWidth={2} strokeLinejoin="round" strokeLinecap="round" />
          )}
          {activePoint && active !== null && (
            <line x1={x(active)} x2={x(active)} y1={pad.top} y2={pad.top + plotH} stroke={MUTED} strokeWidth={1} />
          )}
          {activePoint && active !== null && activePoint.score_pct !== null && (
            <circle cx={x(active)} cy={y(activePoint.score_pct)} r={4} fill={LINE} stroke={SURFACE} strokeWidth={2} />
          )}
          {last && active === null && (
            <>
              <circle cx={x(last.i)} cy={y(last.score_pct as number)} r={4} fill={LINE} stroke={SURFACE} strokeWidth={2} />
              {!compact && (
                <text x={x(last.i) + 8} y={y(last.score_pct as number) + 4} fontSize={12} fontWeight={600} fill="var(--pf-t--global--text--color--regular)">
                  {pct(last.score_pct)}
                </text>
              )}
            </>
          )}
        </svg>
        {activePoint && active !== null && (
          <div
            role="status"
            style={{
              position: "absolute",
              top: 0,
              left: `${Math.min(80, Math.max(0, (x(active) / width) * 100 - 10))}%`,
              background: SURFACE,
              border: `1px solid ${GRID}`,
              borderRadius: 4,
              padding: "0.25rem 0.5rem",
              pointerEvents: "none",
              fontSize: "0.8rem",
              whiteSpace: "nowrap",
              boxShadow: "var(--pf-t--global--box-shadow--sm)",
            }}
          >
            <strong>{pct(activePoint.score_pct)}</strong>{" "}
            <span className="bor-text-secondary">{day(activePoint.generated_at)}</span>
            <div className="bor-text-secondary">
              {activePoint.by_status.non_compliant} non-compliant, {activePoint.by_status.error} errors, {activePoint.nodes} nodes
            </div>
          </div>
        )}
      </div>
      {!compact && (
        <>
          <Button variant="link" isInline onClick={() => setShowTable((v) => !v)} aria-expanded={showTable} aria-controls={`${id}-table`}>
            {showTable ? "Hide table" : "Show as table"}
          </Button>
          {showTable && (
            <div id={`${id}-table`}>
              <Table aria-label={`${title} by day`} variant="compact">
                <Thead>
                  <Tr>
                    <Th>Day</Th>
                    <Th>Score</Th>
                    <Th>Nodes</Th>
                    <Th>Non-compliant</Th>
                    <Th>Errors</Th>
                    <Th>Stale</Th>
                  </Tr>
                </Thead>
                <Tbody>
                  {points.map((p) => (
                    <Tr key={p.report_id}>
                      <Td dataLabel="Day">{new Date(p.generated_at).toLocaleDateString()}</Td>
                      <Td dataLabel="Score">{pct(p.score_pct)}</Td>
                      <Td dataLabel="Nodes">{p.nodes}</Td>
                      <Td dataLabel="Non-compliant">{p.by_status.non_compliant}</Td>
                      <Td dataLabel="Errors">{p.by_status.error}</Td>
                      <Td dataLabel="Stale">{p.results_stale}</Td>
                    </Tr>
                  ))}
                </Tbody>
              </Table>
            </div>
          )}
        </>
      )}
    </div>
  );
};
