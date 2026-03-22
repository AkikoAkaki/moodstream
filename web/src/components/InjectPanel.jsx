import { useState, useCallback } from "react";

const PRESETS = [
  "666",
  "哈哈哈",
  "太厉害了",
  "主播好帅",
  "看不懂",
  "awsl",
  "下次一定",
  "前方高能",
];

export default function InjectPanel() {
  const [videoId, setVideoId] = useState("demo");
  const [rawText, setRawText] = useState("");
  const [timestampMs, setTimestampMs] = useState(1000);
  const [sent, setSent] = useState([]);
  const [sending, setSending] = useState(false);

  const send = useCallback(
    async (text) => {
      const body = {
        video_id: videoId,
        raw_text: text || rawText,
        timestamp_ms: timestampMs,
      };
      if (!body.video_id || !body.raw_text) return;

      setSending(true);
      try {
        const res = await fetch("/events/push", {
          method: "POST",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify(body),
        });
        if (res.ok) {
          setSent((prev) =>
            [{ text: body.raw_text, ts: body.timestamp_ms }, ...prev].slice(
              0,
              30,
            ),
          );
          if (!text) setRawText("");
          setTimestampMs((t) => t + 100);
        }
      } finally {
        setSending(false);
      }
    },
    [videoId, rawText, timestampMs],
  );

  const burst = useCallback(
    async (text, count = 20) => {
      for (let i = 0; i < count; i++) {
        await send(text);
      }
    },
    [send],
  );

  const handleSubmit = (e) => {
    e.preventDefault();
    send();
  };

  return (
    <div className="panel">
      <div className="panel-header">
        <div className="panel-title">Inject Events</div>
        <button className="btn btn-ghost" onClick={() => setSent([])}>
          Clear log
        </button>
      </div>
      <div className="panel-body">
        <form onSubmit={handleSubmit}>
          <div className="form-group">
            <label className="form-label">Video ID</label>
            <input
              className="form-input"
              value={videoId}
              onChange={(e) => setVideoId(e.target.value)}
              placeholder="e.g. demo"
            />
          </div>

          <div className="form-row">
            <div className="form-group">
              <label className="form-label">Timestamp (ms)</label>
              <input
                className="form-input"
                type="number"
                value={timestampMs}
                onChange={(e) => setTimestampMs(Number(e.target.value))}
              />
            </div>
            <div className="form-group">
              <label className="form-label">&nbsp;</label>
              <div className="form-hint" style={{ marginTop: 12 }}>
                Auto-increments on send
              </div>
            </div>
          </div>

          <div className="form-group">
            <label className="form-label">Comment text</label>
            <input
              className="form-input"
              value={rawText}
              onChange={(e) => setRawText(e.target.value)}
              placeholder="Type a danmu comment..."
              autoFocus
            />
          </div>

          <button className="btn btn-primary" type="submit" disabled={sending}>
            Send
          </button>
        </form>

        <div style={{ marginTop: 20 }}>
          <div className="form-label">Quick send (click = x1, long-press or shift+click = x20)</div>
          <div style={{ display: "flex", flexWrap: "wrap", gap: 6, marginTop: 8 }}>
            {PRESETS.map((p) => (
              <button
                key={p}
                className="btn btn-ghost"
                onClick={(e) => (e.shiftKey ? burst(p) : send(p))}
              >
                {p}
              </button>
            ))}
          </div>
        </div>

        {sent.length > 0 && (
          <div className="sent-list">
            <div className="sent-label">Sent ({sent.length})</div>
            {sent.map((s, i) => (
              <div key={i} className="sent-item">
                <span className="sent-text">{s.text}</span>
                <span className="sent-ts">{s.ts}ms</span>
              </div>
            ))}
          </div>
        )}
      </div>
    </div>
  );
}
