/* rain-net 管理台:无依赖单页应用,消费 admin API */
"use strict";

const $ = (sel) => document.querySelector(sel);
const TOKEN_KEY = "star_admin_token";

let token = localStorage.getItem(TOKEN_KEY) || "";
let activeTab = "overview";
let refreshTimer = null;

/* ---------- 工具 ---------- */

function esc(s) {
  return String(s ?? "").replace(/[&<>"']/g, (c) => ({
    "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;",
  }[c]));
}

function fmtBytes(n) {
  if (!n) return "0 B";
  const units = ["B", "KB", "MB", "GB", "TB"];
  let i = 0;
  let v = Number(n);
  while (v >= 1024 && i < units.length - 1) { v /= 1024; i++; }
  return (i === 0 ? v : v.toFixed(1)) + " " + units[i];
}

function fmtTime(iso) {
  if (!iso) return "-";
  const d = new Date(iso);
  if (isNaN(d)) return "-";
  const p = (x) => String(x).padStart(2, "0");
  return `${p(d.getMonth() + 1)}-${p(d.getDate())} ${p(d.getHours())}:${p(d.getMinutes())}:${p(d.getSeconds())}`;
}

function fmtUptime(sec) {
  if (sec == null) return "-";
  const d = Math.floor(sec / 86400);
  const h = Math.floor((sec % 86400) / 3600);
  const m = Math.floor((sec % 3600) / 60);
  const s = sec % 60;
  if (d) return `${d}天${h}时`;
  if (h) return `${h}时${m}分`;
  if (m) return `${m}分${s}秒`;
  return `${s}秒`;
}

function toast(msg, isErr) {
  const el = $("#toast");
  el.textContent = msg;
  el.classList.toggle("err", !!isErr);
  el.classList.add("show");
  clearTimeout(toast._t);
  toast._t = setTimeout(() => el.classList.remove("show"), 2600);
}

function emptyRow(cols, text) {
  return `<tr class="empty"><td colspan="${cols}">${esc(text)}</td></tr>`;
}

/* ---------- API ---------- */

async function api(path, opts = {}) {
  const headers = Object.assign({}, opts.headers);
  if (token) headers["Authorization"] = "Bearer " + token;
  if (opts.body && typeof opts.body !== "string") {
    opts.body = JSON.stringify(opts.body);
    headers["Content-Type"] = "application/json";
  }
  const resp = await fetch(path, Object.assign({}, opts, { headers }));
  if (resp.status === 401) {
    showLogin("Token 无效或已变更");
    throw new Error("unauthorized");
  }
  const text = await resp.text();
  let data = text;
  try { data = JSON.parse(text); } catch (_) { /* 非 JSON(如 405 页面) */ }
  if (!resp.ok) {
    const msg = (data && data.error) ? data.error : "HTTP " + resp.status;
    throw new Error(msg);
  }
  return data;
}

/* ---------- 登录 ---------- */

function showLogin(errText) {
  $("#loginErr").textContent = errText || "";
  $("#loginOverlay").classList.remove("hidden");
  $("#tokenInput").focus();
}

async function tryLogin() {
  const t = $("#tokenInput").value.trim();
  if (!t) return;
  const saved = token;
  token = t;
  try {
    await api("/api/status");
    localStorage.setItem(TOKEN_KEY, t);
    $("#loginOverlay").classList.add("hidden");
    $("#loginErr").textContent = "";
    $("#tokenInput").value = "";
    toast("已连接");
    refresh();
  } catch (e) {
    token = saved;
    if (e.message !== "unauthorized") showLogin("连接失败: " + e.message);
  }
}

$("#loginBtn").addEventListener("click", tryLogin);
$("#tokenInput").addEventListener("keydown", (e) => { if (e.key === "Enter") tryLogin(); });
$("#logoutBtn").addEventListener("click", () => {
  localStorage.removeItem(TOKEN_KEY);
  token = "";
  showLogin("");
});

/* ---------- 渲染 ---------- */

async function renderOverview() {
  const st = await api("/api/status");
  $("#st-clients").textContent = st.clients;
  $("#st-streams").textContent = st.streams;
  $("#st-conns").textContent = st.conns;
  $("#st-in").textContent = fmtBytes(st.inBytes);
  $("#st-out").textContent = fmtBytes(st.outBytes);
  $("#st-uptime").textContent = fmtUptime(st.uptime);
}

async function renderClients() {
  const list = await api("/api/clients");
  const tb = $("#clientsTable tbody");
  if (!list.length) { tb.innerHTML = emptyRow(6, "暂无在线客户端"); return; }
  tb.innerHTML = list.map((c) => `
    <tr>
      <td class="mono">${esc(c.name)}</td>
      <td class="mono">${esc(c.identity || "*")}</td>
      <td class="mono">${esc(c.remote)}</td>
      <td class="mono">${fmtTime(c.since)}</td>
      <td>${c.streams}</td>
      <td><button class="mini danger" data-kick="${esc(c.identity || "")}">踢下线</button></td>
    </tr>`).join("");
}

async function renderStreams() {
  const list = await api("/api/streams");
  const tb = $("#streamsTable tbody");
  if (!list.length) { tb.innerHTML = emptyRow(6, "暂无已注册流"); return; }
  tb.innerHTML = list.map((s) => `
    <tr>
      <td class="mono">${s.assignId}</td>
      <td class="mono">${esc(s.clientProxyName)}</td>
      <td class="mono">${esc(s.streamId)}</td>
      <td class="mono">${fmtTime(s.since)}</td>
      <td class="mono">${fmtBytes(s.inBytes)}</td>
      <td class="mono">${fmtBytes(s.outBytes)}</td>
    </tr>`).join("");
}

async function renderConns() {
  const list = await api("/api/conns");
  const tb = $("#connsTable tbody");
  if (!list.length) { tb.innerHTML = emptyRow(4, "暂无活跃连接"); return; }
  tb.innerHTML = list.map((c) => `
    <tr>
      <td class="mono">${c.connId}</td>
      <td class="mono">${esc(c.clientProxyName)}</td>
      <td class="mono">${esc(c.streamId)}</td>
      <td class="mono">${fmtTime(c.openedAt)}</td>
    </tr>`).join("");
}

async function renderRules() {
  const groups = await api("/api/connects");
  const box = $("#rulesBox");
  const names = Object.keys(groups);
  if (!names.length) { box.innerHTML = emptyRow(1, "暂无路由规则"); return; }
  box.innerHTML = names.sort().map((p) => `
    <div class="rule-group">
      <h4>${esc(p)}</h4>
      <table class="tbl">
        <thead><tr><th>身份</th><th>流标识</th><th></th></tr></thead>
        <tbody>
          ${groups[p].map((r) => `
            <tr>
              <td class="mono">${esc(r.clientProxyName)}</td>
              <td class="mono">${esc(r.streamId)}</td>
              <td><button class="mini danger" data-delrule="${esc(p)}" data-cp="${esc(r.clientProxyName)}" data-sid="${esc(r.streamId)}">删除</button></td>
            </tr>`).join("")}
        </tbody>
      </table>
    </div>`).join("");
}

async function renderCredentials() {
  const list = await api("/api/clientProxies");
  const tb = $("#credsTable tbody");
  if (!list.length) { tb.innerHTML = emptyRow(5, "暂无凭据,可在上方发放"); return; }
  tb.innerHTML = list.map((c) => `
    <tr>
      <td class="mono">${esc(c.clientProxyName)}</td>
      <td class="mono">${esc(c.streamId)}</td>
      <td class="mono">${esc(c.addr)}</td>
      <td class="mono">${esc(c.transport || "tcp")}</td>
      <td>
        <label class="switch"><input type="checkbox" class="kick-chk" data-key="${esc(c.clientProxyName)}/${esc(c.streamId)}"> 同时踢下线</label>
        <button class="mini danger" data-delcred="${esc(c.clientProxyName)}" data-sid="${esc(c.streamId)}">删除</button>
      </td>
    </tr>`).join("");
}

/* ---------- 事件委托(操作按钮) ---------- */

document.addEventListener("click", async (e) => {
  const kick = e.target.closest("[data-kick]");
  if (kick) {
    if (!kick.dataset.kick) { toast("该客户端无身份,无法按身份踢下线", true); return; }
    try {
      const r = await api("/api/kick", { method: "POST", body: { clientProxyName: kick.dataset.kick } });
      toast(`已踢下线 ${r.kicked} 个会话`);
      refresh();
    } catch (err) { toast(err.message, true); }
    return;
  }

  const delRule = e.target.closest("[data-delrule]");
  if (delRule) {
    try {
      await api(`/api/connects/${encodeURIComponent(delRule.dataset.delrule)}/${encodeURIComponent(delRule.dataset.cp)}/${encodeURIComponent(delRule.dataset.sid)}`, { method: "DELETE" });
      toast("路由规则已删除");
      refresh();
    } catch (err) { toast(err.message, true); }
    return;
  }

  const delCred = e.target.closest("[data-delcred]");
  if (delCred) {
    const key = delCred.dataset.delcred + "/" + delCred.dataset.sid;
    const chk = document.querySelector(`.kick-chk[data-key="${CSS.escape(key)}"]`);
    const kickQ = chk && chk.checked ? "?kick=1" : "";
    try {
      const r = await api(`/api/clientProxies/${encodeURIComponent(delCred.dataset.delcred)}/${encodeURIComponent(delCred.dataset.sid)}${kickQ}`, { method: "DELETE" });
      toast(r.kicked != null ? `已删除并踢下线 ${r.kicked} 个会话` : "凭据已删除");
      refresh();
    } catch (err) { toast(err.message, true); }
  }
});

$("#ruleForm").addEventListener("submit", async (e) => {
  e.preventDefault();
  const f = new FormData(e.target);
  try {
    await api("/api/connects", {
      method: "POST",
      body: {
        proxyName: f.get("proxyName").trim(),
        clientProxyName: f.get("clientProxyName").trim(),
        streamId: f.get("streamId").trim(),
      },
    });
    toast("路由规则已添加");
    e.target.reset();
    refresh();
  } catch (err) { toast(err.message, true); }
});

$("#credForm").addEventListener("submit", async (e) => {
  e.preventDefault();
  const f = new FormData(e.target);
  try {
    await api("/api/clientProxies", {
      method: "POST",
      body: {
        clientProxyName: f.get("clientProxyName").trim(),
        streamId: f.get("streamId").trim(),
        addr: f.get("addr").trim(),
        transport: "tcp",
        keyPassword: f.get("keyPassword").trim(),
      },
    });
    toast("凭据已发放,客户端可立即接入");
    e.target.reset();
    refresh();
  } catch (err) { toast(err.message, true); }
});

/* ---------- Tab 与轮询 ---------- */

const renderers = {
  overview: renderOverview,
  clients: renderClients,
  streams: renderStreams,
  conns: renderConns,
  routing: renderRules,
  credentials: renderCredentials,
};

async function refresh() {
  const dot = $("#connDot");
  try {
    await renderers[activeTab]();
    dot.classList.add("ok");
    $("#connText").textContent = "已连接";
  } catch (err) {
    if (err.message !== "unauthorized") {
      dot.classList.remove("ok");
      $("#connText").textContent = "连接异常";
    }
  }
}

document.querySelectorAll("#tabs button").forEach((btn) => {
  btn.addEventListener("click", () => {
    document.querySelectorAll("#tabs button").forEach((b) => b.classList.remove("active"));
    document.querySelectorAll(".tab").forEach((t) => t.classList.remove("active"));
    btn.classList.add("active");
    activeTab = btn.dataset.tab;
    $("#tab-" + activeTab).classList.add("active");
    refresh();
  });
});

function setupPolling() {
  clearInterval(refreshTimer);
  refreshTimer = setInterval(() => {
    if ($("#autoRefresh").checked && $("#loginOverlay").classList.contains("hidden")) {
      refresh();
    }
  }, 4000);
}

/* ---------- 启动 ---------- */

if (token) {
  api("/api/status")
    .then(() => { refresh(); })
    .catch(() => { /* 401 已弹出登录框 */ });
} else {
  showLogin("");
}
setupPolling();
