import { useCallback, useEffect, useRef, useState } from "react";

function newFallbackId() {
  if (globalThis.crypto?.randomUUID) return `evt-${globalThis.crypto.randomUUID()}`;
  return `evt-${Date.now()}-${Math.random().toString(36).slice(2)}`;
}

export function eventToMessage(ev) {
  const ts = ev.ts
    ? (() => {
        const d = new Date(ev.ts);
        return Number.isFinite(d.valueOf())
          ? d.toISOString().split("T")[1].replace("Z", "").slice(0, 12)
          : String(ev.ts);
      })()
    : "";
  const from = ev.from || ev.component || "—";
  const toRaw = ev.to;
  const to = toRaw && String(toRaw).trim() !== "" ? toRaw : ev.kind === "log" ? "—" : from;
  const id =
    ev.id !== undefined && ev.id !== null && String(ev.id).trim() !== "" ? String(ev.id) : newFallbackId();
  return {
    id,
    ts,
    from,
    to: to || "—",
    type: ev.type || ev.detail || ev.msg || "",
    detail: ev.detail || ev.type || "",
    spec: ev.spec || ev.specRef || "",
    kind: ev.kind || "",
    level: ev.level || "",
    fields: ev.fields && typeof ev.fields === "object" ? ev.fields : undefined,
    raw: JSON.stringify(ev, null, 2),
  };
}

export function useObservatory() {
  const [messages, setMessages] = useState([]);
  const [topology, setTopology] = useState({ nfs: [], online: 0, total: 0 });
  const [ues, setUEs] = useState([]);
  const [uptime, setUptime] = useState("—");
  const [connected, setConnected] = useState(false);
  const wsRef = useRef(null);

  const refreshUEs = useCallback(async () => {
    try {
      const r = await fetch("/api/v1/ues");
      if (r.ok) {
        const data = await r.json();
        setUEs(data.ues || []);
      }
    } catch {
      /* observatory offline */
    }
  }, []);

  useEffect(() => {
    const proto = window.location.protocol === "https:" ? "wss:" : "ws:";
    const ws = new WebSocket(`${proto}//${window.location.host}/ws`);
    wsRef.current = ws;

    ws.onopen = () => setConnected(true);
    ws.onclose = () => setConnected(false);
    ws.onmessage = (e) => {
      const msg = JSON.parse(e.data);
      if (msg.type === "snapshot") {
        if (msg.topology) setTopology(msg.topology);
        if (msg.messages) setMessages(msg.messages.map(eventToMessage));
        if (msg.ues) setUEs(msg.ues);
        if (msg.uptime) setUptime(msg.uptime);
      } else if (msg.type === "event" && msg.event) {
        setMessages((prev) => [...prev.slice(-499), eventToMessage(msg.event)]);
      } else if (msg.type === "topology" && msg.topology) {
        setTopology(msg.topology);
      }
    };

    const poll = setInterval(refreshUEs, 5000);
    return () => {
      clearInterval(poll);
      ws.close();
    };
  }, [refreshUEs]);

  const clearMessages = useCallback(async () => {
    setMessages([]);
  }, []);

  const spawnUE = useCallback(async () => {
    const r = await fetch("/api/v1/ues", { method: "POST" });
    if (!r.ok) throw new Error(await r.text());
    await refreshUEs();
    return r.json();
  }, [refreshUEs]);

  const stopUE = useCallback(
    async (id) => {
      const r = await fetch(`/api/v1/ues/${encodeURIComponent(id)}`, { method: "DELETE" });
      if (!r.ok && r.status !== 204) throw new Error(await r.text());
      await refreshUEs();
    },
    [refreshUEs]
  );

  return {
    messages,
    topology,
    ues,
    uptime,
    connected,
    clearMessages,
    spawnUE,
    stopUE,
    refreshUEs,
  };
}
