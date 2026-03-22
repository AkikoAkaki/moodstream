import { useState, useEffect, useRef } from "react";
import InjectPanel from "./components/InjectPanel";
import ResultsPanel from "./components/ResultsPanel";

const MAX_RESULTS = 50;
const SSE_STATUS = { CONNECTING: "connecting", CONNECTED: "connected", ERROR: "error" };

export default function App() {
  const [results, setResults] = useState([]);
  const [sseStatus, setSseStatus] = useState(SSE_STATUS.CONNECTING);
  const eventSourceRef = useRef(null);

  useEffect(() => {
    const es = new EventSource("/stream/results");
    eventSourceRef.current = es;

    es.onopen = () => setSseStatus(SSE_STATUS.CONNECTED);

    es.onmessage = (e) => {
      try {
        const data = JSON.parse(e.data);
        setResults((prev) => {
          const next = [data, ...prev];
          return next.length > MAX_RESULTS ? next.slice(0, MAX_RESULTS) : next;
        });
      } catch {
        /* ignore non-JSON lines (e.g. : connected) */
      }
    };

    es.onerror = () => {
      setSseStatus(SSE_STATUS.ERROR);
      setTimeout(() => {
        if (es.readyState === EventSource.CONNECTING) {
          setSseStatus(SSE_STATUS.CONNECTING);
        }
      }, 1000);
    };

    return () => es.close();
  }, []);

  return (
    <div className="app">
      <header className="app-header">
        <div className="app-title">
          Stream Dashboard <span>real-time audience analysis</span>
        </div>
        <div className="status">
          <span
            className={`status-dot ${sseStatus === SSE_STATUS.CONNECTED ? "connected" : sseStatus === SSE_STATUS.ERROR ? "error" : ""}`}
          />
          {sseStatus === SSE_STATUS.CONNECTED
            ? "Live"
            : sseStatus === SSE_STATUS.ERROR
              ? "Disconnected"
              : "Connecting..."}
        </div>
      </header>
      <main className="app-body">
        <InjectPanel />
        <ResultsPanel results={results} />
      </main>
    </div>
  );
}
