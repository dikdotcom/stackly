package report

// reportCSS is the embedded stylesheet for the HTML report. Kept as a
// const string so the output is self-contained (no external CSS files).
// Style is intentionally minimal — readable in print and on screen.
const reportCSS = `
*{box-sizing:border-box;margin:0;padding:0}
body{font-family:-apple-system,BlinkMacSystemFont,"Segoe UI",Roboto,sans-serif;
  color:#0F172A;background:#FAFAF9;line-height:1.5;padding:32px;max-width:980px;margin:0 auto}
.hdr{background:#0F172A;color:#fff;padding:32px;border-radius:12px;margin-bottom:24px}
.hdr-row{display:flex;justify-content:space-between;align-items:baseline;margin-bottom:24px}
.hdr-brand{font-weight:700;font-size:14px;letter-spacing:0.5px;text-transform:uppercase;color:#94A3B8}
.hdr-meta{font-size:12px;color:#94A3B8;font-family:ui-monospace,monospace}
.hdr-url{font-size:24px;font-weight:600;word-break:break-all;margin-bottom:24px}
.hdr-stats{display:flex;gap:24px;flex-wrap:wrap}
.hdr-stat-label{font-size:11px;text-transform:uppercase;letter-spacing:0.5px;color:#94A3B8;margin-bottom:4px}
.hdr-stat-val{font-size:16px;font-weight:500}
.mono{font-family:ui-monospace,monospace;font-size:13px}
.sec{background:#fff;border:1px solid #E2E8F0;border-radius:12px;padding:24px;margin-bottom:16px}
.sec h2{font-size:16px;font-weight:600;margin-bottom:16px;color:#0F172A}
.cat-block{margin-bottom:16px}
.cat-block:last-child{margin-bottom:0}
.cat-title{font-size:12px;text-transform:uppercase;letter-spacing:0.5px;color:#64748B;
  margin-bottom:8px;font-weight:600}
.pills{display:flex;flex-wrap:wrap;gap:8px}
.pill{display:inline-block;padding:6px 12px;background:#F1F5F9;border-radius:999px;
  font-size:13px;color:#0F172A;font-weight:500}
.pill-link{text-decoration:none;color:#0F172A;border:1px solid #CBD5E1}
.pill-link:hover{background:#E2E8F0}
.pill-conf{color:#64748B;font-weight:400;margin-left:4px}
.grid-2{display:grid;grid-template-columns:repeat(2,1fr);gap:16px}
@media(max-width:680px){.grid-2{grid-template-columns:1fr}}
.card{background:#FAFAF9;border:1px solid #E2E8F0;border-radius:8px;padding:16px}
.card-title{font-size:12px;text-transform:uppercase;letter-spacing:0.5px;color:#64748B;
  font-weight:600;margin-bottom:12px}
.kv{display:flex;justify-content:space-between;padding:6px 0;font-size:13px;
  border-bottom:1px solid #F1F5F9;gap:12px}
.kv:last-child{border-bottom:none}
.k{color:#64748B;font-weight:500}
.v{color:#0F172A;text-align:right;word-break:break-word;font-family:ui-monospace,monospace;font-size:12px}
.v.warn{color:#D97706}
.v.err{color:#DC2626}
.empty{color:#94A3B8;font-size:13px;font-style:italic}
.emails{display:flex;flex-wrap:wrap;gap:8px}
.email-pill{display:inline-block;padding:6px 12px;background:#FEF3C7;color:#92400E;
  border-radius:6px;font-size:13px;text-decoration:none;font-family:ui-monospace,monospace}
.email-pill:hover{background:#FDE68A}
.ftr{text-align:center;color:#94A3B8;font-size:12px;margin-top:32px;padding-top:16px;border-top:1px solid #E2E8F0}
.ftr a{color:#64748B;text-decoration:none}
@media print{
  body{background:#fff;padding:0}
  .hdr{background:#fff;color:#0F172A;border:1px solid #0F172A}
  .hdr-brand,.hdr-meta,.hdr-stat-label{color:#64748B}
  .sec{break-inside:avoid;border:1px solid #E2E8F0}
  .pill-link{color:#0F172A}
}
`
