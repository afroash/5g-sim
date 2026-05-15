export const C = {
  bg: "#080c10",
  surface: "#0d1117",
  border: "#1a2332",
  borderHi: "#243447",
  text: "#c9d8e8",
  muted: "#4a6178",
  dim: "#2a3f54",
  accent: "#00d4ff",
  accentDim: "#004d5e",
  green: "#00ff88",
  greenDim: "#003d20",
  amber: "#ffb800",
  amberDim: "#3d2c00",
  red: "#ff4060",
  redDim: "#3d0010",
  purple: "#a855f7",
  purpleDim: "#2d1050",
};

// SVG layout (coordinates inside viewBox). Extra top/left room so labels and glow are not clipped.
export const TOPO_VIEWBOX = { x: -50, y: -40, w: 940, h: 500 };

export const NF_NODES = [
  { id: "NRF", label: "NRF", sub: "Network Repository Function", x: 420, y: 95, color: C.purple, spec: "TS 29.510" },
  { id: "AMF", label: "AMF", sub: "Access & Mobility Management", x: 180, y: 215, color: C.accent, spec: "TS 29.518" },
  { id: "SMF", label: "SMF", sub: "Session Management Function", x: 420, y: 215, color: C.green, spec: "TS 29.502" },
  { id: "UPF", label: "UPF", sub: "User Plane Function", x: 420, y: 355, color: C.amber, spec: "TS 29.244" },
  { id: "gNB", label: "gNB", sub: "Next-Gen NodeB", x: 180, y: 355, color: C.text, spec: "TS 38.413" },
];

export const NF_EDGES = [
  ["gNB", "AMF"],
  ["AMF", "NRF"],
  ["AMF", "SMF"],
  ["SMF", "UPF"],
  ["NRF", "SMF"],
];

export function nfColor(id) {
  if (!id) return C.text;
  const base = id.split("-")[0];
  return NF_NODES.find((n) => n.id === id || n.id === base)?.color ?? C.text;
}

export function stateColor(s) {
  if (s === "REGISTERED" || s === "CONNECTED") return C.green;
  if (s === "STARTING" || s === "REGISTERING" || s === "ATTACHING") return C.amber;
  if (s === "IDLE") return C.amber;
  return C.red;
}
