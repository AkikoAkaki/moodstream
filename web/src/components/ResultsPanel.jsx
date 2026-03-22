const POSITIVE = new Set([
  "excited",
  "amused",
  "wholesome",
  "happy",
  "admiring",
  "inspired",
  "impressed",
  "supportive",
  "cheerful",
  "joyful",
]);

const NEGATIVE = new Set([
  "angry",
  "frustrated",
  "annoyed",
  "sad",
  "disgusted",
  "hostile",
  "anxious",
  "scared",
  "disappointed",
]);

function emotionVariant(tag) {
  const t = (tag || "").toLowerCase();
  if (POSITIVE.has(t)) return "positive";
  if (NEGATIVE.has(t)) return "negative";
  return "neutral";
}

function formatTime(unixMs) {
  return new Date(unixMs).toLocaleTimeString();
}

export default function ResultsPanel({ results }) {
  if (results.length === 0) {
    return (
      <div className="panel">
        <div className="panel-header">
          <div className="panel-title">Analysis Results</div>
        </div>
        <div className="panel-body">
          <div className="empty">
            <div className="empty-icon">~</div>
            <div>Waiting for analysis results...</div>
            <div>Send some events and wait 5 seconds</div>
          </div>
        </div>
      </div>
    );
  }

  return (
    <div className="panel">
      <div className="panel-header">
        <div className="panel-title">Analysis Results</div>
        <span className="status" style={{ fontSize: 11 }}>
          {results.length} results
        </span>
      </div>
      <div className="panel-body">
        {results.map((r, i) => (
          <div key={r.processed_at} className="result-card">
            <div className="result-header">
              <span className="result-video">{r.video_id}</span>
              <span className="result-time">
                {formatTime(r.processed_at)}
              </span>
            </div>
            <div className={`emotion-badge ${emotionVariant(r.emotion_tag)}`}>
              {r.emotion_tag}
            </div>
            <div className="result-topic">{r.core_topic}</div>
            <div className="result-meta">
              <span>{r.event_count} events</span>
              <span>
                window {r.window_start}ms &ndash; {r.window_end}ms
              </span>
            </div>
          </div>
        ))}
      </div>
    </div>
  );
}
