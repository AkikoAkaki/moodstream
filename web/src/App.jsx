import { useState, useEffect, useRef } from "react";
import InjectPanel from "./components/InjectPanel";
import ResultsPanel from "./components/ResultsPanel";

export default function App() {
  const [results, setResults] = useState([]);
  const [sseStatus, setSseStatus] = useState("connecting");
  const eventSourceRef = useRef(null);

  useEffect(() => {
    const es = new EventSource("/stream/results");
    eventSourceRef.current = es;

    es.onopen = () => setSseStatus("connected");

    es.onmessage = (e) => {
      try {
        const data = JSON.parse(e.data);
        setResults((prev) => [data, ...prev].slice(0, 50));
      } catch {
        /* ignore non-JSON lines (e.g. : connected) */
      }
    };

    es.onerror = () => {
      setSseStatus("error");
      // EventSource auto-reconnects
      setTimeout(() => {
        if (es.readyState === EventSource.CONNECTING) {
          setSseStatus("connecting");
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
            className={`status-dot ${sseStatus === "connected" ? "connected" : sseStatus === "error" ? "error" : ""}`}
          />
          {sseStatus === "connected"
            ? "Live"
            : sseStatus === "error"
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
