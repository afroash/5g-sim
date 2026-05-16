import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { C, NF_NODES, NF_EDGES, TOPO_VIEWBOX, nfColor, stateColor } from "./theme.js";
import { useObservatory } from "./useObservatory.js";

function TopologyCanvas({ messages, selectedNF, onSelectNF, nfStatus }) {
  const [pulses, setPulses] = useState([]);
  const pulseId = useRef(0);

  useEffect(() => {
    if (!messages.length) return;
    const last = messages[messages.length - 1];
    const fromNode = NF_NODES.find(
      (n) => n.id === last.from || n.id.startsWith(String(last.from).split("-")[0])
    );
    const toNode = NF_NODES.find(
      (n) => n.id === last.to || n.id.startsWith(String(last.to).split("-")[0])
    );
    if (!fromNode || !toNode || toNode.id === "—") return;
    const id = ++pulseId.current;
    setPulses((p) => [...p.slice(-8), { id, from: fromNode, to: toNode, color: nfColor(last.from), t: 0 }]);
    const timer = setTimeout(() => setPulses((p) => p.filter((x) => x.id !== id)), 1200);
    return () => clearTimeout(timer);
  }, [messages]);

  const statusMap = Object.fromEntries((nfStatus || []).map((n) => [n.id, n.status]));

  const vb = TOPO_VIEWBOX;
  const gx0 = vb.x;
  const gx1 = vb.x + vb.w;
  const vbStr = `${vb.x} ${vb.y} ${vb.w} ${vb.h}`;
  return (
    <svg
      viewBox={vbStr}
      preserveAspectRatio="xMidYMid meet"
      overflow="visible"
      style={{
        width: "100%",
        height: "100%",
        minHeight: 280,
        maxHeight: "100%",
        display: "block",
        fontFamily: "'JetBrains Mono', monospace",
      }}
    >
      <defs>
        <filter id="glow">
          <feGaussianBlur stdDeviation="3" result="blur" />
          <feMerge>
            <feMergeNode in="blur" />
            <feMergeNode in="SourceGraphic" />
          </feMerge>
        </filter>
        {NF_NODES.map((n) => (
          <radialGradient key={n.id} id={`grad-${n.id}`} cx="50%" cy="50%" r="50%">
            <stop offset="0%" stopColor={n.color} stopOpacity="0.15" />
            <stop offset="100%" stopColor={n.color} stopOpacity="0" />
          </radialGradient>
        ))}
      </defs>
      {Array.from({ length: 11 }).map((_, i) => (
        <line
          key={`h${i}`}
          x1={gx0}
          y1={vb.y + i * 50}
          x2={gx1}
          y2={vb.y + i * 50}
          stroke={C.border}
          strokeWidth="0.5"
          opacity="0.35"
        />
      ))}
      {NF_EDGES.map(([a, b]) => {
        const A = NF_NODES.find((n) => n.id === a),
          B = NF_NODES.find((n) => n.id === b);
        return (
          <line
            key={`${a}-${b}`}
            x1={A.x}
            y1={A.y}
            x2={B.x}
            y2={B.y}
            stroke={C.borderHi}
            strokeWidth="1.5"
            strokeDasharray="4 6"
            opacity="0.6"
          />
        );
      })}
      {pulses.map((pulse) => (
        <PulseAnim key={pulse.id} pulse={pulse} />
      ))}
      {NF_NODES.map((node) => {
        const sel = selectedNF === node.id;
        const up = statusMap[node.id] === "up";
        return (
          <g key={node.id} style={{ cursor: "pointer" }} onClick={() => onSelectNF(sel ? null : node.id)}>
            <circle cx={node.x} cy={node.y} r="48" fill={`url(#grad-${node.id})`} />
            <rect
              x={node.x - 32}
              y={node.y - 28}
              width="64"
              height="56"
              rx="8"
              fill={C.surface}
              stroke={sel ? node.color : C.borderHi}
              strokeWidth={sel ? 2 : 1}
              filter={sel ? "url(#glow)" : ""}
            />
            <text x={node.x} y={node.y - 6} textAnchor="middle" fill={node.color} fontSize="15" fontWeight="700">
              {node.label}
            </text>
            <text x={node.x} y={node.y + 12} textAnchor="middle" fill={C.muted} fontSize="7">
              {node.spec}
            </text>
            <circle
              cx={node.x + 26}
              cy={node.y - 22}
              r="5"
              fill={up ? C.green : C.red}
              filter="url(#glow)"
            />
          </g>
        );
      })}
      <g opacity={0.75}>
        <text x={420} y={438} textAnchor="middle" fill={C.muted} fontSize="10">
          data network (N6)
        </text>
        <line x1={420} y1={422} x2={420} y2={382} stroke={C.borderHi} strokeWidth="1.5" strokeDasharray="4 6" />
      </g>
    </svg>
  );
}

function PulseAnim({ pulse }) {
  const [t, setT] = useState(0);
  useEffect(() => {
    const start = Date.now();
    const tick = () => {
      const elapsed = (Date.now() - start) / 1200;
      setT(Math.min(elapsed, 1));
      if (elapsed < 1) requestAnimationFrame(tick);
    };
    requestAnimationFrame(tick);
  }, [pulse.id]);
  const x = pulse.from.x + (pulse.to.x - pulse.from.x) * t;
  const y = pulse.from.y + (pulse.to.y - pulse.from.y) * t;
  const op = t < 0.1 ? t * 10 : t > 0.8 ? (1 - t) * 5 : 1;
  return (
    <g>
      <circle cx={x} cy={y} r="7" fill={pulse.color} opacity={op * 0.3} />
      <circle cx={x} cy={y} r="4" fill={pulse.color} opacity={op} />
    </g>
  );
}

function MessageLog({ messages, onSelectId, selectedId }) {
  const endRef = useRef(null);
  useEffect(() => {
    endRef.current?.scrollIntoView({ behavior: "smooth" });
  }, [messages]);
  return (
    <div style={{ display: "flex", flexDirection: "column", height: "100%", minHeight: 0, overflow: "hidden" }}>
      <div style={{ padding: "10px 14px", borderBottom: `1px solid ${C.border}`, display: "flex", alignItems: "center", gap: 8 }}>
        <span style={{ width: 8, height: 8, borderRadius: "50%", background: C.green, display: "inline-block" }} />
        <span style={{ color: C.muted, fontSize: 11, letterSpacing: "0.1em", textTransform: "uppercase" }}>Message Log</span>
        <span style={{ marginLeft: "auto", color: C.dim, fontSize: 10 }}>{messages.length} events</span>
      </div>
      <div style={{ flex: 1, overflowY: "auto", padding: "4px 0" }}>
        {messages.map((m, i) => (
          <div
            key={m.id ? `${m.id}:${i}` : `row-${i}`}
            onClick={() => onSelectId(selectedId === m.id ? null : m.id)}
            style={{
              padding: "6px 14px",
              cursor: "pointer",
              borderLeft: `2px solid ${selectedId === m.id ? nfColor(m.from) : "transparent"}`,
              background: selectedId === m.id ? `${nfColor(m.from)}10` : "transparent",
            }}
          >
            <div style={{ display: "flex", gap: 8, alignItems: "center", marginBottom: 2 }}>
              <span style={{ color: C.dim, fontSize: 9, minWidth: 70 }}>{m.ts}</span>
              <span style={{ color: nfColor(m.from), fontSize: 10, fontWeight: 700 }}>{m.from}</span>
              {m.to && m.to !== "—" && (
                <>
                  <span style={{ color: C.dim, fontSize: 10 }}>→</span>
                  <span style={{ color: nfColor(m.to), fontSize: 10, fontWeight: 700 }}>{m.to}</span>
                </>
              )}
            </div>
            <div style={{ color: C.text, fontSize: 11 }}>{m.type}</div>
            {m.spec && <div style={{ color: C.muted, fontSize: 10 }}>{m.spec}</div>}
          </div>
        ))}
        <div ref={endRef} />
      </div>
    </div>
  );
}

function inspectBody(msg) {
  if (!msg) return "";
  if (typeof msg.raw === "string" && msg.raw.trim() !== "") return msg.raw;
  try {
    return JSON.stringify(
      {
        ts: msg.ts,
        from: msg.from,
        to: msg.to,
        type: msg.type,
        detail: msg.detail,
        spec: msg.spec,
        fields: msg.fields,
      },
      null,
      2
    );
  } catch {
    return String(msg.type || "");
  }
}

function MessageDetail({ msg, onClose }) {
  if (!msg)
    return (
      <div style={{ height: "100%", display: "flex", alignItems: "center", justifyContent: "center", color: C.dim, fontSize: 12 }}>
        Select a message to inspect
      </div>
    );
  const rawText = inspectBody(msg);
  const fieldEntries = msg.fields && typeof msg.fields === "object" ? Object.entries(msg.fields) : [];
  return (
    <div style={{ height: "100%", minHeight: 0, display: "flex", flexDirection: "column" }}>
      <div style={{ padding: "10px 14px", borderBottom: `1px solid ${C.border}`, display: "flex", flexShrink: 0 }}>
        <span style={{ color: C.muted, fontSize: 11, textTransform: "uppercase" }}>Protocol Inspector</span>
        <button type="button" onClick={onClose} style={{ marginLeft: "auto", background: "none", border: "none", color: C.muted, cursor: "pointer" }}>
          ✕
        </button>
      </div>
      <div style={{ flex: 1, overflowY: "auto", padding: 14 }}>
        <div style={{ marginBottom: 12, fontSize: 11 }}>
          <span style={{ color: nfColor(msg.from), fontWeight: 700 }}>{msg.from}</span>
          {msg.to && msg.to !== "—" && (
            <>
              {" "}
              <span style={{ color: C.dim }}>→</span>{" "}
              <span style={{ color: nfColor(msg.to), fontWeight: 700 }}>{msg.to}</span>
            </>
          )}
        </div>
        <div style={{ marginBottom: 6, fontSize: 12, color: C.text }}>{msg.type || msg.detail || "—"}</div>
        {msg.spec ? (
          <div style={{ marginBottom: 12, fontSize: 10, color: C.purple }}>
            {msg.spec}
          </div>
        ) : null}
        {fieldEntries.length > 0 && (
          <>
            <div style={{ color: C.muted, fontSize: 9, textTransform: "uppercase", marginBottom: 6 }}>Fields</div>
            <div style={{ marginBottom: 12 }}>
              {fieldEntries.map(([k, v]) => (
                <div key={k} style={{ fontSize: 10, marginBottom: 4 }}>
                  <span style={{ color: C.dim }}>{k}:</span> <span style={{ color: C.accent }}>{String(v)}</span>
                </div>
              ))}
            </div>
          </>
        )}
        <div style={{ color: C.muted, fontSize: 9, textTransform: "uppercase", marginBottom: 6 }}>Raw payload</div>
        <pre
          style={{
            whiteSpace: "pre-wrap",
            wordBreak: "break-word",
            background: C.bg,
            border: `1px solid ${C.border}`,
            borderRadius: 6,
            padding: 12,
            color: C.green,
            fontSize: 10,
            margin: 0,
            minHeight: 80,
          }}
        >
          {rawText}
        </pre>
      </div>
    </div>
  );
}

function UEPanel({ ues, onAdd, onDetach, selected, onSelect, spawnProfile, onSpawnProfileChange }) {
  return (
    <div style={{ height: "100%", minHeight: 0, display: "flex", flexDirection: "column" }}>
      <div style={{ padding: "10px 14px", borderBottom: `1px solid ${C.border}`, display: "flex", alignItems: "center", flexWrap: "wrap", gap: 8 }}>
        <span style={{ color: C.muted, fontSize: 11, textTransform: "uppercase" }}>User Equipment</span>
        <span style={{ marginLeft: 8, color: C.dim, fontSize: 10 }}>{ues.length} devices</span>
        <div style={{ marginLeft: "auto", display: "flex", alignItems: "center", gap: 8 }}>
          <label style={{ display: "flex", alignItems: "center", gap: 6, fontSize: 10, color: C.dim }}>
            profile
            <select
              value={spawnProfile}
              onChange={(e) => onSpawnProfileChange(e.target.value)}
              style={{
                padding: "3px 8px",
                background: C.surface,
                border: `1px solid ${C.border}`,
                borderRadius: 4,
                color: C.text,
                fontSize: 10,
                fontFamily: "inherit",
                cursor: "pointer",
              }}
            >
              <option value="local">local</option>
              <option value="clab">clab</option>
            </select>
          </label>
          <button
            onClick={onAdd}
            style={{
              padding: "4px 12px",
              background: C.accentDim,
              border: `1px solid ${C.accent}`,
              borderRadius: 4,
              color: C.accent,
              fontSize: 11,
              cursor: "pointer",
              fontFamily: "inherit",
            }}
          >
            + Add UE
          </button>
        </div>
      </div>
      <div style={{ flex: 1, overflowY: "auto" }}>
        {ues.length === 0 && (
          <div style={{ padding: 20, color: C.dim, fontSize: 11 }}>No UEs — start one with + Add UE or run cmd/ue</div>
        )}
        {ues.map((ue) => (
          <div
            key={ue.id}
            onClick={() => onSelect(selected?.id === ue.id ? null : ue)}
            style={{
              padding: "10px 14px",
              cursor: "pointer",
              borderLeft: `2px solid ${selected?.id === ue.id ? stateColor(ue.state) : "transparent"}`,
            }}
          >
            <div style={{ display: "flex", alignItems: "center" }}>
              <span style={{ fontWeight: 700, fontSize: 12 }}>{ue.id}</span>
              <span
                style={{
                  marginLeft: 8,
                  padding: "1px 7px",
                  borderRadius: 3,
                  background: `${stateColor(ue.state)}18`,
                  color: stateColor(ue.state),
                  fontSize: 9,
                }}
              >
                {ue.state}
              </span>
              {ue.source === "spawned" && (
                <button
                  onClick={(e) => {
                    e.stopPropagation();
                    onDetach(ue.id);
                  }}
                  style={{ marginLeft: "auto", background: "none", border: `1px solid ${C.red}`, color: C.red, borderRadius: 4, fontSize: 10, cursor: "pointer" }}
                >
                  Stop
                </button>
              )}
            </div>
            <div style={{ fontSize: 10, color: C.muted, marginTop: 4 }}>
              IMSI {ue.imsi} {ue.ip ? `· ${ue.ip}` : ""}
              {ue.source === "spawned" && ue.profile ? ` · ${ue.profile}` : ""}
            </div>
          </div>
        ))}
      </div>
    </div>
  );
}

function StatsBar({ topology, messages, ues, uptime, connected }) {
  const registered = ues.filter((u) => u.state === "REGISTERED" || u.state === "CONNECTED").length;
  const online = topology.online ?? 0;
  const total = topology.total ?? NF_NODES.length;
  return (
    <div style={{ display: "flex", gap: 24, padding: "8px 18px", borderBottom: `1px solid ${C.border}`, background: C.surface }}>
      <Stat label="NFs Online" value={`${online}/${total}`} color={online === total ? C.green : C.amber} />
      <Stat label="Registered UEs" value={registered} color={C.accent} />
      <Stat label="Messages" value={messages.length} color={C.purple} />
      <Stat label="Uptime" value={uptime} color={C.muted} />
      <div style={{ marginLeft: "auto", display: "flex", alignItems: "center", gap: 6 }}>
        <span style={{ width: 7, height: 7, borderRadius: "50%", background: connected ? C.green : C.red }} />
        <span style={{ color: connected ? C.green : C.red, fontSize: 10 }}>
          {connected ? "OBSERVATORY LIVE" : "DISCONNECTED"}
        </span>
      </div>
    </div>
  );
}

function Stat({ label, value, color }) {
  return (
    <div>
      <div style={{ color: C.dim, fontSize: 8, textTransform: "uppercase" }}>{label}</div>
      <div style={{ color: color, fontSize: 14, fontWeight: 700 }}>{value}</div>
    </div>
  );
}

export default function App() {
  const { messages, topology, ues, uptime, connected, clearMessages, spawnUE, stopUE } = useObservatory();
  const [selectedMsgId, setSelectedMsgId] = useState(null);
  const selectedMsg = useMemo(() => messages.find((m) => m.id === selectedMsgId) ?? null, [messages, selectedMsgId]);
  const [selectedNF, setSelectedNF] = useState(null);
  const [selectedUE, setSelectedUE] = useState(null);
  const [tab, setTab] = useState("topology");
  const [err, setErr] = useState(null);

  const [spawnProfile, setSpawnProfile] = useState("local");

  const handleAddUE = useCallback(async () => {
    try {
      setErr(null);
      await spawnUE({ profile: spawnProfile });
    } catch (e) {
      setErr(String(e.message || e));
    }
  }, [spawnUE, spawnProfile]);

  const handleDetach = useCallback(
    async (id) => {
      try {
        setErr(null);
        await stopUE(id);
        setSelectedUE(null);
      } catch (e) {
        setErr(String(e.message || e));
      }
    },
    [stopUE]
  );

  const rightPanel = selectedNF ? (
    <NFDetail nfId={selectedNF} messages={messages} onClose={() => setSelectedNF(null)} topology={topology} />
  ) : (
    <MessageDetail msg={selectedMsg} onClose={() => setSelectedMsgId(null)} />
  );

  return (
    <div
      style={{
        width: "100%",
        height: "100vh",
        background: C.bg,
        color: C.text,
        fontFamily: "'JetBrains Mono', monospace",
        display: "flex",
        flexDirection: "column",
        overflow: "hidden",
      }}
    >
      <div style={{ padding: "10px 20px", borderBottom: `1px solid ${C.border}`, background: C.surface, display: "flex", alignItems: "center" }}>
        <span style={{ color: C.accent, fontSize: 14, fontWeight: 700 }}>5G-SIM</span>
        <span style={{ color: C.dim, fontSize: 10, marginLeft: 8 }}>NETWORK OBSERVATORY</span>
        <div style={{ marginLeft: "auto", display: "flex", gap: 6 }}>
          {["topology", "ues"].map((t) => (
            <button
              key={t}
              onClick={() => setTab(t)}
              style={{
                padding: "4px 14px",
                background: tab === t ? C.accentDim : "transparent",
                border: `1px solid ${tab === t ? C.accent : C.border}`,
                borderRadius: 4,
                color: tab === t ? C.accent : C.muted,
                fontSize: 11,
                cursor: "pointer",
                fontFamily: "inherit",
              }}
            >
              {t === "topology" ? "Topology" : "UE Fleet"}
            </button>
          ))}
        </div>
      </div>
      {err && <div style={{ padding: "6px 18px", background: C.redDim, color: C.red, fontSize: 11 }}>{err}</div>}
      <StatsBar topology={topology} messages={messages} ues={ues} uptime={uptime} connected={connected} />
      <div style={{ padding: "6px 14px", borderBottom: `1px solid ${C.border}`, display: "flex", gap: 8 }}>
        <button
          type="button"
          onClick={() => {
            clearMessages();
            setSelectedMsgId(null);
          }}
          style={{ padding: "4px 12px", background: "transparent", border: `1px solid ${C.border}`, borderRadius: 4, color: C.muted, fontSize: 11, cursor: "pointer", fontFamily: "inherit" }}
        >
          Clear log (local)
        </button>
        <span style={{ color: C.dim, fontSize: 10, alignSelf: "center" }}>Run NFs with OBSERVATORY_URL set to stream events</span>
      </div>
      <div style={{ flex: 1, display: "grid", gridTemplateColumns: "1fr 280px 280px", overflow: "hidden", minHeight: 0 }}>
        <div style={{ borderRight: `1px solid ${C.border}`, padding: 12, minHeight: 0, display: "flex", flexDirection: "column", overflow: "hidden" }}>
          {tab === "topology" ? (
            <div style={{ flex: 1, minHeight: 340, overflow: "hidden", display: "flex" }}>
              <TopologyCanvas
                messages={messages}
                selectedNF={selectedNF}
                onSelectNF={(id) => {
                  setSelectedNF(id);
                  if (id) setSelectedMsgId(null);
                }}
                nfStatus={topology.nfs}
              />
            </div>
          ) : (
            <UEPanel
              ues={ues}
              onAdd={handleAddUE}
              onDetach={handleDetach}
              selected={selectedUE}
              onSelect={setSelectedUE}
              spawnProfile={spawnProfile}
              onSpawnProfileChange={setSpawnProfile}
            />
          )}
        </div>
        <div style={{ borderRight: `1px solid ${C.border}`, minHeight: 0, display: "flex", flexDirection: "column" }}>
          <MessageLog
            messages={messages}
            onSelectId={(id) => {
              setSelectedMsgId(id);
              if (id) setSelectedNF(null);
            }}
            selectedId={selectedMsgId}
          />
        </div>
        <div style={{ minHeight: 0, overflow: "hidden", display: "flex", flexDirection: "column" }}>
          {rightPanel}
        </div>
      </div>
    </div>
  );
}

function NFDetail({ nfId, messages, onClose, topology }) {
  const node = NF_NODES.find((n) => n.id === nfId);
  const st = topology.nfs?.find((n) => n.id === nfId);
  const related = messages.filter((m) => m.from === nfId || m.to === nfId).slice(-6);
  if (!node) return null;
  return (
    <div style={{ height: "100%", minHeight: 0, display: "flex", flexDirection: "column" }}>
      <div style={{ padding: "10px 14px", borderBottom: `1px solid ${C.border}`, display: "flex", alignItems: "center" }}>
        <span style={{ color: node.color, fontWeight: 700 }}>{nfId}</span>
        <button onClick={onClose} style={{ marginLeft: "auto", background: "none", border: "none", color: C.muted, cursor: "pointer" }}>
          ✕
        </button>
      </div>
      <div style={{ padding: 14, overflowY: "auto", flex: 1 }}>
        <div style={{ fontSize: 12, marginBottom: 8 }}>{node.sub}</div>
        <div style={{ fontSize: 11, color: C.muted }}>
          Status: <span style={{ color: st?.status === "up" ? C.green : C.red }}>{st?.status ?? "unknown"}</span>
        </div>
        <div style={{ marginTop: 12, fontSize: 10, color: C.dim }}>Recent messages</div>
        {related.map((m) => (
          <div key={m.id} style={{ padding: 6, borderBottom: `1px solid ${C.border}`, fontSize: 10 }}>
            {m.from} → {m.to}: {m.type}
          </div>
        ))}
      </div>
    </div>
  );
}
