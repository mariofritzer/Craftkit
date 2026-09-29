"use strict";

const TOKEN = document.querySelector('meta[name="craftkit-token"]').content;
const LOADERS = {
  vanilla:  { name: "Vanilla",  ico: "🌱", desc: "Unverändertes Minecraft, keine Mods." },
  fabric:   { name: "Fabric",   ico: "🧵", desc: "Leicht und schnell, riesige Auswahl für neue Versionen." },
  forge:    { name: "Forge",    ico: "⚒",  desc: "Der Klassiker, viele große Mods und Modpacks." },
  neoforge: { name: "NeoForge", ico: "🔥", desc: "Moderner Forge-Nachfolger ab Minecraft 1.20.2." },
  quilt:    { name: "Quilt",    ico: "🧶", desc: "Fabric-Ableger, lädt auch die meisten Fabric-Mods." },
};
const PLATFORMS = {
  paper: "Paper", purpur: "Purpur", spigot: "Spigot", bukkit: "Bukkit", folia: "Folia",
  velocity: "Velocity (Proxy)", bungeecord: "BungeeCord (Proxy)", waterfall: "Waterfall (Proxy)",
};
const SOURCES = { modrinth: "Modrinth", curseforge: "CurseForge" };

const S = {
  state: null,
  view: { type: "welcome" },
  cart: [],          // {source, projectId, versionId, versionLabel, name, iconUrl}
  cartTarget: null,  // {type, id}
  source: "modrinth",
};

// ---------- helpers ----------
const $ = (sel, root = document) => root.querySelector(sel);
const $$ = (sel, root = document) => [...root.querySelectorAll(sel)];
function esc(s) {
  return String(s ?? "").replace(/[&<>"']/g, c => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" }[c]));
}
async function api(path, body) {
  const opts = { headers: { "X-CraftKit-Token": TOKEN } };
  if (body !== undefined) {
    opts.method = "POST";
    opts.headers["Content-Type"] = "application/json";
    opts.body = JSON.stringify(body);
  }
  let res;
  try { res = await fetch(path, opts); }
  catch (e) { throw new Error("CraftKit ist nicht mehr erreichbar – bitte neu starten."); }
  const txt = await res.text();
  let data = null;
  try { data = txt ? JSON.parse(txt) : null; } catch { data = null; }
  if (!res.ok) throw new Error((data && data.error) || txt || ("HTTP " + res.status));
  return data;
}
function toast(msg, err = false) {
  const t = document.createElement("div");
  t.className = "toast" + (err ? " err" : "");
  t.textContent = msg;
  $("#toasts").appendChild(t);
  setTimeout(() => t.remove(), err ? 7000 : 4000);
}
function fmtNum(n) {
  if (!n) return "0";
  if (n >= 1e6) return (n / 1e6).toFixed(n >= 1e7 ? 0 : 1).replace(".", ",") + " Mio.";
  if (n >= 1e3) return Math.round(n / 1e3) + " Tsd.";
  return String(n);
}
function fmtDate(s) {
  if (!s) return "";
  const d = new Date(s);
  return isNaN(d) ? "" : d.toLocaleDateString("de-AT", { day: "2-digit", month: "2-digit", year: "numeric" });
}
function iconHTML(url, name, cls = "mod-icon") {
  if (url) return `<img class="${cls}" src="${esc(url)}" alt="" loading="lazy" referrerpolicy="no-referrer" onerror="this.replaceWith(Object.assign(document.createElement('div'),{className:'${cls}',textContent:'${esc((name || "?")[0]).toUpperCase()}'}))">`;
  return `<div class="${cls}">${esc((name || "?")[0].toUpperCase())}</div>`;
}
function loading(text = "Lade …") { return `<div class="loading"><span class="spinner"></span>${esc(text)}</div>`; }
function openExternal(url) { api("/api/open-url", { url }).catch(e => toast(e.message, true)); }
document.addEventListener("click", e => {
  const a = e.target.closest("a[data-ext]");
  if (a) { e.preventDefault(); openExternal(a.getAttribute("href")); }
});

// ---------- keep-alive ----------
function ping() { fetch("/api/ping", { method: "POST", headers: { "X-CraftKit-Token": TOKEN } }).catch(() => {}); }
setInterval(ping, 20000);
ping();
window.addEventListener("pagehide", () => navigator.sendBeacon("/api/bye?t=" + TOKEN));

// ---------- state ----------
async function refreshState() {
  const [st, disc] = await Promise.all([api("/api/state"), api("/api/discover").catch(() => ({ profiles: [], versions: [] }))]);
  S.state = st;
  S.discover = disc;
  renderSidebar();
  renderTopStatus();
}

function renderTopStatus() {
  const st = S.state;
  const parts = [];
  if (st.launcherRunning) parts.push(`<span class="pill pill-gold" title="Neue Profile erscheinen erst nach einem Neustart des Launchers"><span class="dot"></span>Launcher läuft</span>`);
  if (!st.launcherLabel) parts.push(`<span class="pill pill-red" title="Pfad in den Einstellungen angeben"><span class="dot"></span>Launcher nicht gefunden</span>`);
  $("#topStatus").innerHTML = parts.join("");
}

function renderSidebar() {
  const st = S.state;
  const v = S.view;
  const insts = st.instances || [];
  $("#instanceList").innerHTML = insts.length ? insts.map(i => `
    <button class="side-item ${v.type === "instance" && v.id === i.id ? "active" : ""}" data-inst="${esc(i.id)}">
      <span class="side-ico ld-${esc(i.loader)}">${LOADERS[i.loader]?.ico || "?"}</span>
      <span class="side-text"><div class="side-title">${esc(i.name)}</div>
      <div class="side-meta">${esc(LOADERS[i.loader]?.name)} · ${esc(i.mcVersion)}${i.loader !== "vanilla" ? " · " + i.modCount + " Mods" : ""}</div></span>
    </button>`).join("") : `<div class="side-empty">Noch keine Instanz. Klick auf +.</div>`;
  const pfs = st.pluginFolders || [];
  $("#pluginList").innerHTML = pfs.length ? pfs.map(p => `
    <button class="side-item ${v.type === "plugins" && v.id === p.id ? "active" : ""}" data-pf="${esc(p.id)}">
      <span class="side-ico ld-plugin">🔌</span>
      <span class="side-text"><div class="side-title">${esc(p.name)}</div>
      <div class="side-meta">${esc(PLATFORMS[p.platform] || p.platform)}${p.mcVersion ? " · " + esc(p.mcVersion) : ""} · ${p.count} Plugins</div></span>
    </button>`).join("") : `<div class="side-empty">Kein Plugin-Ordner. Klick auf +.</div>`;
  const found = (S.discover?.profiles || []).filter(p => !p.instanceId);
  $("#foundSection").classList.toggle("hidden", !found.length);
  $("#foundList").innerHTML = found.map(p => `
    <button class="side-item ${v.type === "found" && v.id === p.key ? "active" : ""}" data-found="${esc(p.key)}">
      <span class="side-ico ${LOADERS[p.loader] ? "ld-" + esc(p.loader) : ""}">${LOADERS[p.loader]?.ico || "◇"}</span>
      <span class="side-text"><div class="side-title">${esc(p.name)}</div>
      <div class="side-meta">${esc(loaderLabel(p.loader))}${p.mcVersion ? " · " + esc(p.mcVersion) : ""}${p.modCount ? " · " + p.modCount + " Mods" : ""}</div></span>
    </button>`).join("");
  $$("[data-found]").forEach(b => b.onclick = () => go({ type: "found", id: b.dataset.found }));
  $$("[data-inst]").forEach(b => b.onclick = () => go({ type: "instance", id: b.dataset.inst }));
  $$("[data-pf]").forEach(b => b.onclick = () => go({ type: "plugins", id: b.dataset.pf }));
  $("#btnSettings").classList.toggle("active", v.type === "settings");
}

function loaderLabel(l) {
  return LOADERS[l]?.name || (l === "optifine" ? "OptiFine" : "Unbekannt");
}

function go(view) {
  S.view = view;
  const t = (view.type === "instance" || view.type === "plugins") ? { type: view.type, id: view.id } : null;
  if (!t || !S.cartTarget || S.cartTarget.type !== t.type || S.cartTarget.id !== t.id) {
    S.cart = [];
    S.cartTarget = t;
  }
  renderCart();
  renderSidebar();
  render();
}

function render() {
  const v = S.view;
  const m = $("#main");
  m.scrollTop = 0;
  if (v.type === "new") return renderNewInstance(m);
  if (v.type === "newPlugins") return renderNewPluginFolder(m);
  if (v.type === "instance" || v.type === "plugins") return renderTarget(m, v.type, v.id);
  if (v.type === "settings") return renderSettings(m);
  if (v.type === "found") return renderFound(m, v.id);
  renderWelcome(m);
}

// ---------- welcome ----------
function renderWelcome(m) {
  const st = S.state;
  m.innerHTML = `<div class="main-inner">
    ${!st.minecraftFound ? `<div class="banner banner-warn"><span class="b-ico">⚠</span><div>Kein Minecraft-Ordner unter <span class="mono">${esc(st.config.minecraftDir)}</span> gefunden. Starte den offiziellen Launcher einmal oder passe den Pfad in den Einstellungen an.</div></div>` : ""}
    <div class="hero">
      <h1>Minecraft einrichten, ohne Dateien zu schieben.</h1>
      <p>CraftKit installiert Minecraft-Versionen mit Forge, NeoForge, Fabric oder Quilt, lädt Mods und Plugins von Modrinth und CurseForge und nimmt alle Voraussetzungen automatisch mit. Gespielt wird wie gewohnt im offiziellen Launcher.</p>
      <div style="display:flex;gap:10px;margin-top:18px;flex-wrap:wrap">
        <button class="btn btn-primary" id="wNew">+ Neue Instanz anlegen</button>
        <button class="btn" id="wSrv">🌐 Für einen Server einrichten</button>
        <button class="btn" id="wPf">Plugin-Ordner hinzufügen</button>
      </div>
      <div class="feature-grid">
        <div class="feature"><b>1 · Version wählen</b><span>Loader und Minecraft-Version aussuchen, CraftKit installiert alles und legt ein eigenes Profil an.</span></div>
        <div class="feature"><b>2 · Mods aussuchen</b><span>Suchen, in den Korb legen, Abhängigkeiten werden vor der Installation angezeigt.</span></div>
        <div class="feature"><b>3 · Spielen</b><span>„Minecraft Launcher öffnen“ drücken und das Profil mit „(CraftKit)“ im Namen starten.</span></div>
      </div>
    </div></div>`;
  $("#wNew").onclick = () => go({ type: "new" });
  $("#wPf").onclick = () => go({ type: "newPlugins" });
  $("#wSrv").onclick = () => serverModal(null);
}

// ---------- new instance wizard ----------
const W = { loader: "fabric", mc: "", lv: "", name: "", memory: 4, snapshots: false, mcList: null, lvList: null, err: "" };

async function renderNewInstance(m) {
  W.snapshots = S.state.config.showSnapshots;
  m.innerHTML = `<div class="main-inner">
    <div class="page-head"><div class="grow"><h1>Neue Instanz</h1>
      <div class="sub">Jede Instanz hat einen eigenen Ordner für Mods, Welten und Einstellungen und erscheint als eigenes Profil im Minecraft Launcher.</div></div></div>
    ${S.state.launcherRunning ? `<div class="banner banner-warn"><span class="b-ico">⚠</span><div>Der Minecraft Launcher ist gerade offen. Schließ ihn am besten vorher, sonst taucht das neue Profil erst nach einem Neustart des Launchers auf.</div></div>` : ""}
    <div class="step-title done"><span class="step-num">1</span>Loader</div>
    <div class="loader-grid" id="wLoaders"></div>
    <div class="step-title"><span class="step-num">2</span>Version</div>
    <div class="card card-pad"><div class="form-grid">
      <div class="field"><label>Minecraft-Version</label><select class="input" id="wMc"></select>
        <label class="check" style="margin-top:4px"><input type="checkbox" id="wSnap"> Snapshots &amp; ältere Typen anzeigen</label></div>
      <div class="field" id="wLvField"><label id="wLvLabel">Loader-Version</label><select class="input" id="wLv"></select><span class="hint" id="wLvHint"></span></div>
      <div class="field"><label>Name</label><input class="input" id="wName" maxlength="60" placeholder="z. B. Fabric 1.21 mit Freunden"></div>
      <div class="field"><label>Arbeitsspeicher (RAM)</label>
        <div class="range-row"><input type="range" id="wMem" min="0" max="16" step="1"><span class="range-val" id="wMemVal"></span></div>
        <span class="hint">„Standard“ überlässt es dem Launcher. Für größere Modpacks 6–8 GB.</span></div>
    </div></div>
    <div id="wErr"></div>
    <div style="display:flex;justify-content:flex-end;gap:10px;margin-top:18px">
      <button class="btn btn-ghost" id="wCancel">Abbrechen</button>
      <button class="btn btn-primary" id="wCreate">Installieren &amp; Profil anlegen</button>
    </div></div>`;
  $("#wLoaders").innerHTML = Object.entries(LOADERS).map(([k, l]) => `
    <button class="loader-card ${W.loader === k ? "active" : ""}" data-loader="${k}">
      <span class="lc-ico ld-${k}">${l.ico}</span><span class="lc-name">${l.name}</span><span class="lc-desc">${l.desc}</span>
    </button>`).join("");
  $$("[data-loader]").forEach(b => b.onclick = () => {
    W.loader = b.dataset.loader;
    $$("[data-loader]").forEach(x => x.classList.toggle("active", x === b));
    loadMcVersions();
  });
  $("#wSnap").checked = W.snapshots;
  $("#wSnap").onchange = () => { W.snapshots = $("#wSnap").checked; loadMcVersions(); };
  $("#wMc").onchange = () => { W.mc = $("#wMc").value; loadLoaderVersions(); suggestName(); };
  $("#wLv").onchange = () => { W.lv = $("#wLv").value; };
  $("#wName").oninput = () => { W.nameTouched = true; };
  $("#wMem").value = W.memory;
  const memLabel = () => $("#wMemVal").textContent = +$("#wMem").value === 0 ? "Standard" : $("#wMem").value + " GB";
  $("#wMem").oninput = () => { W.memory = +$("#wMem").value; memLabel(); };
  memLabel();
  $("#wCancel").onclick = () => go({ type: "welcome" });
  $("#wCreate").onclick = createInstanceClicked;
  W.nameTouched = false;
  loadMcVersions();
}

function suggestName() {
  if (W.nameTouched) return;
  $("#wName").value = W.mc ? `${LOADERS[W.loader].name} ${W.mc}` : "";
}

async function loadMcVersions() {
  const sel = $("#wMc");
  sel.innerHTML = `<option>Lade Versionen …</option>`;
  sel.disabled = true;
  $("#wErr").innerHTML = "";
  try {
    const list = await api(`/api/game-versions?loader=${W.loader}&snapshots=${W.snapshots ? 1 : 0}`);
    if (!list || !list.length) throw new Error("Keine Versionen gefunden.");
    const prev = W.mc;
    const have = new Set((S.discover?.versions || []).filter(x => x.loader === W.loader).map(x => x.mcVersion));
    sel.innerHTML = list.map(v => `<option value="${esc(v.id)}">${esc(v.id)}${v.type !== "release" ? " (" + esc(v.type) + ")" : ""}${have.has(v.id) ? "  ✓ installiert" : ""}</option>`).join("");
    W.mc = list.some(v => v.id === prev) ? prev : list[0].id;
    sel.value = W.mc;
    sel.disabled = false;
    suggestName();
    loadLoaderVersions();
  } catch (e) {
    sel.innerHTML = `<option>–</option>`;
    $("#wErr").innerHTML = `<div class="banner banner-err" style="margin-top:14px"><span class="b-ico">✕</span><div>Versionen konnten nicht geladen werden: ${esc(e.message)}</div></div>`;
  }
}

async function loadLoaderVersions() {
  const field = $("#wLvField");
  if (W.loader === "vanilla") { field.style.visibility = "hidden"; W.lv = ""; return; }
  field.style.visibility = "visible";
  $("#wLvLabel").textContent = LOADERS[W.loader].name + "-Version";
  const sel = $("#wLv");
  sel.innerHTML = `<option>Lade …</option>`;
  sel.disabled = true;
  $("#wLvHint").textContent = "";
  try {
    const list = await api(`/api/loader-versions?loader=${W.loader}&mc=${encodeURIComponent(W.mc)}`);
    if (!list || !list.length) throw new Error(`Keine ${LOADERS[W.loader].name}-Version für ${W.mc}.`);
    const haveLv = new Set((S.discover?.versions || []).filter(x => x.loader === W.loader && x.mcVersion === W.mc).map(x => x.loaderVersion));
    const isHave = v => haveLv.has(v.version) || (W.loader === "forge" && haveLv.has(v.version.split("-").slice(1).join("-")));
    sel.innerHTML = list.map(v => `<option value="${esc(v.version)}">${esc(v.version)}${v.recommended ? "  ★ empfohlen" : ""}${!v.stable ? "  (Beta)" : ""}${isHave(v) ? "  ✓ installiert" : ""}</option>`).join("");
    const rec = list.find(v => v.recommended) || list[0];
    W.lv = rec.version;
    sel.value = W.lv;
    sel.disabled = false;
    $("#wLvHint").textContent = `${list.length} Versionen verfügbar, die empfohlene ist vorausgewählt.`;
  } catch (e) {
    sel.innerHTML = `<option>–</option>`;
    W.lv = "";
    $("#wLvHint").textContent = e.message;
  }
}

async function createInstanceClicked() {
  const req = { name: $("#wName").value.trim(), mcVersion: W.mc, loader: W.loader, loaderVersion: W.lv, memoryGB: W.memory };
  if (!req.mcVersion) return toast("Bitte eine Minecraft-Version wählen.", true);
  if (req.loader !== "vanilla" && !req.loaderVersion) return toast("Bitte eine Loader-Version wählen.", true);
  try {
    const { job } = await api("/api/instances/create", req);
    jobModal(job, `${LOADERS[req.loader].name} ${req.mcVersion} wird installiert`, async (res) => {
      await refreshState();
      if (res && res.id) {
        go({ type: "instance", id: res.id });
        toast("Fertig! Das Profil heißt im Launcher „" + res.name + " (CraftKit)“.");
      }
    });
  } catch (e) { toast(e.message, true); }
}

// ---------- plugin folder ----------
async function renderNewPluginFolder(m, edit) {
  const pf = edit || { name: "", path: "", platform: "paper", mcVersion: "" };
  m.innerHTML = `<div class="main-inner">
    <div class="page-head"><div class="grow"><h1>${edit ? "Plugin-Ordner bearbeiten" : "Plugin-Ordner hinzufügen"}</h1>
      <div class="sub">Plugins laufen auf Servern (Paper, Spigot, Purpur …). Wähle den <b>plugins</b>-Ordner deines Servers – CraftKit legt die Plugins samt Voraussetzungen dort ab.</div></div></div>
    <div class="card card-pad"><div class="form-grid">
      <div class="field" style="grid-column:1/-1"><label>Ordner</label>
        <div style="display:flex;gap:8px"><input class="input" id="pPath" style="flex:1" placeholder="z. B. C:\\Server\\plugins" value="${esc(pf.path)}" ${edit ? "disabled" : ""}>
        ${edit ? "" : `<button class="btn" id="pPick">Durchsuchen …</button>`}</div></div>
      <div class="field"><label>Name</label><input class="input" id="pName" value="${esc(pf.name)}" placeholder="z. B. Survival-Server"></div>
      <div class="field"><label>Server-Software</label><select class="input" id="pPlat">
        ${Object.entries(PLATFORMS).map(([k, n]) => `<option value="${k}" ${pf.platform === k ? "selected" : ""}>${n}</option>`).join("")}</select></div>
      <div class="field"><label>Minecraft-Version des Servers</label><select class="input" id="pMc"><option value="">beliebig</option></select>
        <span class="hint">Damit nur passende Plugin-Versionen gewählt werden.</span></div>
    </div></div>
    <div style="display:flex;justify-content:flex-end;gap:10px;margin-top:18px">
      <button class="btn btn-ghost" id="pCancel">Abbrechen</button>
      <button class="btn btn-primary" id="pSave">${edit ? "Speichern" : "Hinzufügen"}</button>
    </div></div>`;
  api("/api/game-versions?loader=vanilla&snapshots=0").then(list => {
    $("#pMc").innerHTML = `<option value="">beliebig</option>` + list.map(v => `<option ${v.id === pf.mcVersion ? "selected" : ""}>${esc(v.id)}</option>`).join("");
  }).catch(() => {});
  if (!edit) $("#pPick").onclick = async () => {
    try {
      const r = await api("/api/pick-folder", { title: "plugins-Ordner des Servers wählen" });
      if (r.path) {
        $("#pPath").value = r.path;
        if (!$("#pName").value) {
          const parts = r.path.split(/[\\/]/).filter(Boolean);
          const last = parts[parts.length - 1] || "";
          $("#pName").value = last.toLowerCase() === "plugins" && parts.length > 1 ? parts[parts.length - 2] : last;
        }
      }
    } catch (e) { toast("Ordnerauswahl nicht möglich – bitte Pfad eintippen. (" + e.message + ")", true); }
  };
  $("#pCancel").onclick = () => edit ? go({ type: "plugins", id: edit.id }) : go({ type: "welcome" });
  $("#pSave").onclick = async () => {
    const body = { id: pf.id, name: $("#pName").value.trim(), path: $("#pPath").value.trim(), platform: $("#pPlat").value, mcVersion: $("#pMc").value };
    try {
      const r = await api(edit ? "/api/plugin-folders/update" : "/api/plugin-folders/add", body);
      await refreshState();
      go({ type: "plugins", id: r.id || pf.id });
    } catch (e) { toast(e.message, true); }
  };
}

// ---------- target (instance / plugin folder) ----------
async function renderTarget(m, type, id, tab) {
  S.view.tab = tab || S.view.tab || "installed";
  m.innerHTML = `<div class="main-inner">${loading()}</div>`;
  let data;
  try { data = await api(`/api/target?type=${type}&id=${encodeURIComponent(id)}`); }
  catch (e) {
    // vanilla instances have no mod target, show info page instead
    const inst = (S.state.instances || []).find(i => i.id === id);
    if (type === "instance" && inst) return renderVanilla(m, inst, e.message);
    m.innerHTML = `<div class="main-inner"><div class="banner banner-err"><span class="b-ico">✕</span><div>${esc(e.message)}</div></div></div>`;
    return;
  }
  if (S.view.id !== id) return;
  const t = data.target;
  const inst = data.instance;
  const pf = type === "plugins" ? (S.state.pluginFolders || []).find(p => p.id === id) : null;
  const noun = t.kind === "plugin" ? "Plugins" : "Mods";
  const items = (data.items || []).sort((a, b) => (b.explicit - a.explicit) || a.name.localeCompare(b.name));

  const headIco = inst ? `<div class="head-ico ld-${esc(inst.loader)}">${LOADERS[inst.loader].ico}</div>` : `<div class="head-ico ld-plugin">🔌</div>`;
  const meta = inst
    ? `<span class="pill">${esc(LOADERS[inst.loader].name)} ${esc(inst.loaderVersion)}</span><span class="pill">Minecraft ${esc(inst.mcVersion)}</span>${inst.memoryGB ? `<span class="pill">${inst.memoryGB} GB RAM</span>` : ""}<span class="pill pill-green" title="Name im offiziellen Launcher"><span class="dot"></span>Profil: ${esc(inst.name)}${inst.adopted ? "" : " (CraftKit)"}</span>`
    + (inst.serverAddress ? `<span class="pill" id="srvPill" style="cursor:pointer" title="Server prüfen">🌐 ${esc(inst.serverAddress)} <span class="spinner" style="width:11px;height:11px;border-width:2px"></span></span>` : "")
    : `<span class="pill">${esc(PLATFORMS[pf?.platform] || pf?.platform)}</span><span class="pill">Minecraft ${esc(pf?.mcVersion || "beliebig")}</span>${pf && !pf.exists ? `<span class="pill pill-red">Ordner fehlt</span>` : ""}`;

  m.innerHTML = `<div class="main-inner">
    <div class="page-head">${headIco}
      <div class="grow"><h1>${esc(t.name)}</h1><div class="head-meta">${meta}</div></div>
      <div class="actions">
        ${inst ? `<button class="btn btn-sm" id="tUpgrade" title="Neue Instanz mit anderer Minecraft-Version, Mods werden übernommen">⬆ Version wechseln</button><button class="btn btn-sm" id="tServer">🌐 Server</button>` : ""}
        <button class="btn btn-sm" id="tFolder">📁 Ordner</button>
        <button class="btn btn-sm" id="tUpdate" ${items.length ? "" : "disabled"}>↻ Alle aktualisieren</button>
        <button class="btn btn-sm" id="tEdit">Bearbeiten</button>
        <button class="btn btn-sm btn-danger" id="tDelete">${inst ? "Löschen" : "Entfernen"}</button>
      </div>
    </div>
    <div class="tabs">
      <button class="tab ${S.view.tab === "installed" ? "active" : ""}" data-tab="installed">Installiert<span class="count">${items.length}</span></button>
      <button class="tab ${S.view.tab === "add" ? "active" : ""}" data-tab="add">${noun} hinzufügen</button>
    </div>
    <div id="tabBody"></div></div>`;

  $$("[data-tab]").forEach(b => b.onclick = () => renderTarget(m, type, id, b.dataset.tab));
  $("#tFolder").onclick = () => api("/api/open-folder", { path: inst ? inst.dir : t.dir }).catch(e => toast(e.message, true));
  if (inst) $("#tServer").onclick = () => serverModal(inst);
  if (inst) $("#tUpgrade").onclick = () => upgradeModal(inst);
  if (inst && inst.serverAddress) checkServerPill(inst);
  $("#tUpdate").onclick = () => makePlan({ type, id }, [], true);
  $("#tEdit").onclick = () => inst ? instanceSettingsModal(inst) : renderNewPluginFolder(m, pf);
  $("#tDelete").onclick = () => inst ? deleteInstanceModal(inst) : deletePluginFolder(pf);

  const body = $("#tabBody");
  if (S.view.tab === "installed") renderInstalled(body, t, items, data.foreignInfo || []);
  else renderSearch(body, t);
}

function renderVanilla(m, inst) {
  m.innerHTML = `<div class="main-inner">
    <div class="page-head"><div class="head-ico ld-vanilla">🌱</div>
      <div class="grow"><h1>${esc(inst.name)}</h1><div class="head-meta"><span class="pill">Vanilla</span><span class="pill">Minecraft ${esc(inst.mcVersion)}</span><span class="pill pill-green"><span class="dot"></span>Profil: ${esc(inst.name)} (CraftKit)</span></div></div>
      <div class="actions"><button class="btn btn-sm" id="vUp">⬆ Version wechseln</button><button class="btn btn-sm" id="vSrv">🌐 Server</button><button class="btn btn-sm" id="vEdit">Bearbeiten</button><button class="btn btn-sm btn-danger" id="vDel">Löschen</button></div></div>
    <div class="banner banner-info"><span class="b-ico">ℹ</span><div>Vanilla-Instanzen laden keine Mods. Wenn du Mods willst, leg eine neue Instanz mit Fabric, Forge, NeoForge oder Quilt an. Die Spieldateien lädt der Minecraft Launcher beim ersten Start.</div></div>
    <button class="btn btn-primary" id="vNew">+ Neue Instanz mit Mod-Loader</button></div>`;
  $("#vEdit").onclick = () => instanceSettingsModal(inst);
  $("#vSrv").onclick = () => serverModal(inst);
  $("#vUp").onclick = () => upgradeModal(inst);
  $("#vDel").onclick = () => deleteInstanceModal(inst);
  $("#vNew").onclick = () => { W.mc = inst.mcVersion; go({ type: "new" }); };
}

function renderInstalled(body, t, items, foreign) {
  const noun = t.kind === "plugin" ? "Plugins" : "Mods";
  if (!items.length && !foreign.length) {
    body.innerHTML = `<div class="card empty"><div class="big">📦</div><div>Noch keine ${noun} installiert.</div>
      <div style="margin-top:14px"><button class="btn btn-primary" id="goAdd">${noun} suchen</button></div></div>`;
    $("#goAdd").onclick = () => renderTarget($("#main"), t.type, t.id, "add");
    return;
  }
  const byKey = Object.fromEntries(items.map(i => [i.key, i]));
  const neededBy = key => items.filter(i => (i.dependencies || []).includes(key)).map(i => i.name);
  const row = it => {
    const nb = neededBy(it.key);
    const deps = (it.dependencies || []).map(k => byKey[k]?.name).filter(Boolean);
    return `<div class="row ${it.disabled ? "row-off" : ""}">
      ${toggleHTML(!it.disabled, `data-tkey="${esc(it.key)}"`)}
      ${iconHTML(it.iconUrl, it.name)}
      <div class="grow">
        <div class="row-title">${esc(it.name)}
          ${it.disabled ? `<span class="pill">deaktiviert</span>` : ""}
          ${!it.explicit ? `<span class="pill pill-blue" title="Automatisch als Voraussetzung installiert">Abhängigkeit</span>` : ""}
          <span class="pill">${esc(SOURCES[it.source])}</span></div>
        <div class="row-meta"><span class="mono">${esc(it.versionNumber)}</span> · ${esc(it.fileName)}
          ${nb.length ? ` · benötigt von ${esc(nb.join(", "))}` : ""}${deps.length ? ` · braucht ${esc(deps.join(", "))}` : ""}</div>
      </div>
      <div class="row-actions">
        ${it.pageUrl ? `<a class="btn btn-sm btn-ghost" href="${esc(it.pageUrl)}" data-ext>Seite</a>` : ""}
        <button class="btn btn-sm btn-danger" data-remove="${esc(it.key)}">Entfernen</button>
      </div></div>`;
  };
  const explicit = items.filter(i => i.explicit), auto = items.filter(i => !i.explicit);
  const loaderMismatch = f => t.kind === "mod" && f.loader !== "unknown" && !t.loaders.includes(f.loader);
  body.innerHTML = `
    <div id="missingBox"></div>
    ${explicit.length ? `<div class="section-label" style="margin-top:0">Von dir gewählt · ${explicit.length}</div><div class="card list">${explicit.map(row).join("")}</div>` : ""}
    ${auto.length ? `<div class="section-label">Automatisch mitinstalliert · ${auto.length}</div><div class="card list">${auto.map(row).join("")}</div>` : ""}
    ${foreign.length ? `<div class="section-label" style="display:flex;align-items:center;gap:10px">Nicht über CraftKit installiert · ${foreign.length}
        <span style="flex:1"></span><button class="btn btn-sm" id="btnIdentify" title="Sucht die Dateien per Prüfsumme auf Modrinth und CurseForge">🔎 Online erkennen</button></div>
      <div class="card list">${foreign.map(f => `<div class="row ${f.disabled ? "row-off" : ""}">${toggleHTML(!f.disabled, `data-tfile="${esc(f.file)}"`)}${iconHTML("", f.name)}<div class="grow">
        <div class="row-title">${esc(f.name)}${f.disabled ? `<span class="pill">deaktiviert</span>` : ""}${loaderMismatch(f) ? `<span class="pill pill-red" title="Diese Datei ist für einen anderen Loader">für ${esc(loaderLabel(f.loader))}</span>` : ""}</div>
        <div class="row-meta">${f.version ? `<span class="mono">${esc(f.version)}</span> · ` : ""}${esc(f.file)}${(f.depends || []).length ? " · braucht " + esc(f.depends.filter(d => !["minecraft", "java", "fabricloader", "forge", "neoforge", "quilt_loader"].includes(d)).join(", ") || "–") : ""}</div></div>
        <button class="btn btn-sm btn-danger" data-foreign="${esc(f.file)}">Löschen</button></div>`).join("")}</div>
      <div class="muted" style="font-size:12.5px;margin-top:6px">Erkannte Dateien werden danach wie eigene Installationen verwaltet: mit Updates und Abhängigkeitsprüfung.</div>` : ""}`;
  $$("[data-remove]", body).forEach(b => b.onclick = () => removeFlow(t, byKey[b.dataset.remove]));
  $$("[data-tkey]", body).forEach(b => b.onchange = () => toggleFlow(t, { key: b.dataset.tkey, name: byKey[b.dataset.tkey].name }, b.checked, b));
  $$("[data-tfile]", body).forEach(b => b.onchange = () => toggleFlow(t, { file: b.dataset.tfile, name: b.dataset.tfile }, b.checked, b));
  $$("[data-foreign]", body).forEach(b => b.onclick = () => confirmModal("Datei löschen?", `„${esc(b.dataset.foreign)}“ wird aus dem Ordner gelöscht.`, "Löschen", async () => {
    await api("/api/remove-foreign", { type: t.type, id: t.id, file: b.dataset.foreign });
    renderTarget($("#main"), t.type, t.id, "installed");
  }));
  if ($("#btnIdentify")) $("#btnIdentify").onclick = async () => {
    try {
      const { job } = await api("/api/identify", { type: t.type, id: t.id });
      jobModal(job, "Dateien werden erkannt", async (r) => {
        toast(`${(r.recognized || []).length} erkannt, ${(r.unknown || []).length} unbekannt.`);
        await refreshState();
        renderTarget($("#main"), t.type, t.id, "installed");
      });
    } catch (e) { toast(e.message, true); }
  };
  loadMissing(t);
}

// Checks all jars in the folder (also ones not from CraftKit) for missing dependencies.
async function loadMissing(t, boxSel = "#missingBox") {
  let miss;
  try { miss = await api(`/api/missing?type=${t.type}&id=${encodeURIComponent(t.id)}`); } catch { return; }
  const box = $(boxSel);
  if (!box || !miss || !miss.length) return;
  box.innerHTML = missingBanner(miss);
  const fix = $("[data-fixmissing]", box);
  if (fix) fix.onclick = () => makePlan({ type: t.type, id: t.id }, miss.filter(m => m.projectId).map(m => ({ source: m.source, projectId: m.projectId })));
}

function missingBanner(miss) {
  const fixable = miss.filter(m => m.projectId);
  return `<div class="banner banner-warn"><span class="b-ico">⚠</span><div style="flex:1">
    <b>Fehlende Abhängigkeiten gefunden</b> – ohne sie starten diese Mods vermutlich nicht:
    <ul style="margin:6px 0 0;padding-left:18px">${miss.map(m => `<li><b>${esc(m.name || m.modId)}</b>${m.name && m.name.toLowerCase() !== m.modId.toLowerCase() ? ` <span class="mono muted">(${esc(m.modId)})</span>` : ""} – benötigt von ${esc(m.neededBy.join(", "))}${m.projectId ? "" : ` <span class="muted">· nicht automatisch gefunden</span>`}</li>`).join("")}</ul>
    ${fixable.length ? `<button class="btn btn-sm btn-primary" style="margin-top:10px" data-fixmissing>Fehlende installieren (${fixable.length})</button>` : ""}
  </div></div>`;
}

function toggleHTML(on, attr) {
  return `<label class="switch" title="${on ? "Aktiv – klicken zum Deaktivieren" : "Deaktiviert – klicken zum Aktivieren"}"><input type="checkbox" ${on ? "checked" : ""} ${attr}><span></span></label>`;
}

async function toggleFlow(t, what, enabled, box) {
  const doIt = async () => {
    await api("/api/toggle", { type: t.type, id: t.id, key: what.key || "", file: what.file || "", enabled });
    toast(`„${what.name}“ ${enabled ? "aktiviert" : "deaktiviert"}.`);
    await refreshState();
    renderTarget($("#main"), t.type, t.id, "installed");
  };
  try {
    if (!enabled && what.key) {
      const chk = await api(`/api/remove-check?type=${t.type}&id=${encodeURIComponent(t.id)}&key=${encodeURIComponent(what.key)}`);
      if ((chk.dependents || []).length) {
        box.checked = true;
        confirmModal("Trotzdem deaktivieren?", `<div class="banner banner-warn"><span class="b-ico">⚠</span><div><b>${esc(chk.dependents.join(", "))}</b> ${chk.dependents.length === 1 ? "benötigt" : "benötigen"} „${esc(what.name)}“ und ${chk.dependents.length === 1 ? "startet" : "starten"} ohne sie vermutlich nicht.</div></div>`, "Deaktivieren", doIt);
        return;
      }
    }
    await doIt();
  } catch (e) { box.checked = !enabled; toast(e.message, true); }
}

async function removeFlow(t, it) {
  let chk;
  try { chk = await api(`/api/remove-check?type=${t.type}&id=${encodeURIComponent(t.id)}&key=${encodeURIComponent(it.key)}`); }
  catch (e) { return toast(e.message, true); }
  const dep = chk.dependents || [], orph = chk.orphans || [];
  let html = `<p>„${esc(it.name)}“ wird entfernt.</p>`;
  if (dep.length) html += `<div class="banner banner-warn"><span class="b-ico">⚠</span><div><b>${esc(dep.join(", "))}</b> ${dep.length === 1 ? "benötigt" : "benötigen"} diese ${t.kind === "plugin" ? "Erweiterung" : "Mod"}. Ohne sie ${dep.length === 1 ? "startet diese" : "starten diese"} wahrscheinlich nicht mehr.</div></div>`;
  if (orph.length) html += `<label class="check"><input type="checkbox" id="rmOrph" checked> Nicht mehr benötigte Abhängigkeiten ebenfalls entfernen: ${esc(orph.join(", "))}</label>`;
  confirmModal(dep.length ? "Trotzdem entfernen?" : "Entfernen?", html, "Entfernen", async () => {
    const withOrphans = !!($("#rmOrph") && $("#rmOrph").checked);
    const r = await api("/api/remove", { type: t.type, id: t.id, key: it.key, withOrphans });
    toast("Entfernt: " + (r.removed || []).join(", "));
    await refreshState();
    renderTarget($("#main"), t.type, t.id, "installed");
  });
}

// ---------- search ----------
function renderSearch(body, t) {
  const noun = t.kind === "plugin" ? "Plugins" : "Mods";
  body.innerHTML = `
    <div class="searchbar">
      <div class="seg" id="srcSeg">${Object.entries(SOURCES).map(([k, n]) => `<button data-src="${k}" class="${S.source === k ? "active" : ""}">${n}</button>`).join("")}</div>
      <input class="input search-input" id="q" placeholder="${noun} suchen, z. B. ${t.kind === "plugin" ? "EssentialsX, LuckPerms" : "Sodium, JEI, Create"} …" autocomplete="off">
    </div>
    <div class="muted" style="font-size:12.5px;margin:-4px 0 12px">Zeigt nur ${noun}, die zu ${esc(t.loaders.join(" / "))}${t.kind === "mod" ? " und Minecraft " + esc(t.mcVersion) : ""} passen. Voraussetzungen werden beim Installieren automatisch ergänzt.</div>
    <div id="results"></div>`;
  $$("[data-src]", body).forEach(b => b.onclick = () => {
    S.source = b.dataset.src;
    $$("[data-src]", body).forEach(x => x.classList.toggle("active", x === b));
    doSearch(t, $("#q").value, 0);
  });
  let timer;
  $("#q").oninput = () => { clearTimeout(timer); timer = setTimeout(() => doSearch(t, $("#q").value, 0), 350); };
  $("#q").focus();
  doSearch(t, "", 0);
}

let searchSeq = 0;
async function doSearch(t, q, offset) {
  const seq = ++searchSeq;
  const box = $("#results");
  if (!box) return;
  if (offset === 0) box.innerHTML = loading("Suche …");
  let res;
  try {
    res = await api(`/api/search?source=${S.source}&type=${t.type}&id=${encodeURIComponent(t.id)}&q=${encodeURIComponent(q)}&offset=${offset}`);
  } catch (e) {
    if (seq !== searchSeq) return;
    const isKey = /API-Key/i.test(e.message);
    box.innerHTML = `<div class="banner banner-err"><span class="b-ico">✕</span><div>${esc(e.message)}${isKey ? ` <button class="linkish" id="goSet">Zu den Einstellungen</button>` : ""}</div></div>`;
    if (isKey) $("#goSet").onclick = () => go({ type: "settings" });
    return;
  }
  if (seq !== searchSeq) return;
  const hits = res.hits || [];
  if (offset === 0 && !hits.length) {
    box.innerHTML = `<div class="card empty"><div class="big">🔍</div>Nichts gefunden. Probier einen anderen Begriff oder die andere Quelle.</div>`;
    return;
  }
  let grid = $(".results", box);
  if (offset === 0 || !grid) { box.innerHTML = `<div class="results"></div><div id="more" style="text-align:center;margin-top:14px"></div>`; grid = $(".results", box); }
  hits.forEach(h => grid.insertAdjacentHTML("beforeend", resultCard(h)));
  bindResults(grid, t, hits);
  const shown = $$(".result", grid).length;
  $("#more").innerHTML = shown < res.total ? `<button class="btn" id="moreBtn">Mehr laden (${fmtNum(res.total - shown)} weitere)</button>` : "";
  if ($("#moreBtn")) $("#moreBtn").onclick = () => doSearch(t, q, shown);
}

function inCart(source, id) { return S.cart.find(c => c.source === source && c.projectId === id); }

function resultCard(h) {
  const sel = inCart(h.source, h.id);
  return `<div class="result ${sel ? "selected" : ""}" data-rid="${esc(h.source + ":" + h.id)}">
    ${iconHTML(h.iconUrl, h.name)}
    <div class="grow">
      <div class="result-title">${esc(h.name)}</div>
      <div class="result-sum">${esc(h.summary)}</div>
      <div class="result-foot">
        <span>⬇ ${fmtNum(h.downloads)}</span>${h.author ? `<span>· ${esc(h.author)}</span>` : ""}
        <span class="spacer"></span>
        ${h.installed ? `<span class="pill pill-green">installiert</span>` : ""}
        <button class="linkish" data-ver>${sel && sel.versionLabel ? esc(sel.versionLabel) : "Version"}</button>
        ${h.pageUrl ? `<a class="linkish" href="${esc(h.pageUrl)}" data-ext>Seite</a>` : ""}
        <button class="btn btn-sm ${sel ? "btn-primary" : ""}" data-add>${sel ? "✓ Ausgewählt" : h.installed ? "Neu installieren" : "+ Auswählen"}</button>
      </div>
    </div></div>`;
}

function bindResults(grid, t, hits) {
  hits.forEach(h => {
    const card = $(`[data-rid="${CSS.escape(h.source + ":" + h.id)}"]`, grid);
    if (!card || card.dataset.bound) return;
    card.dataset.bound = "1";
    const refresh = () => { card.outerHTML = resultCard(h); bindResults(grid, t, [h]); renderCart(); };
    $("[data-add]", card).onclick = () => {
      const c = inCart(h.source, h.id);
      if (c) S.cart = S.cart.filter(x => x !== c);
      else S.cart.push({ source: h.source, projectId: h.id, name: h.name, iconUrl: h.iconUrl });
      refresh();
    };
    $("[data-ver]", card).onclick = () => versionPicker(t, h, v => {
      let c = inCart(h.source, h.id);
      if (!c) { c = { source: h.source, projectId: h.id, name: h.name, iconUrl: h.iconUrl }; S.cart.push(c); }
      c.versionId = v ? v.id : "";
      c.versionLabel = v ? v.number : "";
      refresh();
    });
  });
}

async function versionPicker(t, h, onPick) {
  const md = modal(`<div class="modal-head"><div style="flex:1"><h2>Version von ${esc(h.name)}</h2><div class="sub">Nur Versionen, die zu dieser ${t.kind === "plugin" ? "Server-Software" : "Instanz"} passen.</div></div></div>
    <div class="modal-body" id="vpBody">${loading()}</div>
    <div class="modal-foot"><button class="btn btn-ghost" data-close>Abbrechen</button></div>`);
  try {
    const list = await api(`/api/versions?source=${h.source}&type=${t.type}&id=${encodeURIComponent(t.id)}&project=${encodeURIComponent(h.id)}`);
    if (!list || !list.length) { $("#vpBody", md).innerHTML = `<div class="empty">Keine passende Version gefunden.</div>`; return; }
    $("#vpBody", md).innerHTML = `<div class="list card">
      <div class="row" style="cursor:pointer" data-v=""><div class="grow"><div class="row-title">Automatisch (neueste stabile)</div><div class="row-meta">empfohlen</div></div></div>
      ${list.map((v, i) => `<div class="row" style="cursor:pointer" data-v="${i}"><div class="grow">
        <div class="row-title"><span class="mono">${esc(v.number)}</span>${v.type !== "release" ? `<span class="pill pill-gold">${esc(v.type)}</span>` : ""}</div>
        <div class="row-meta">${fmtDate(v.date)} · ${esc((v.gameVersions || []).slice(0, 6).join(", "))}${(v.deps || []).filter(d => d.kind === "required").length ? " · " + v.deps.filter(d => d.kind === "required").length + " Voraussetzung(en)" : ""}</div>
      </div></div>`).join("")}</div>`;
    $$("[data-v]", md).forEach(r => r.onclick = () => { onPick(r.dataset.v === "" ? null : list[+r.dataset.v]); closeModal(md); });
  } catch (e) { $("#vpBody", md).innerHTML = `<div class="banner banner-err"><span class="b-ico">✕</span><div>${esc(e.message)}</div></div>`; }
}

// ---------- cart ----------
function renderCart() {
  const bar = $("#cartBar");
  if (!S.cart.length || !S.cartTarget) { bar.classList.add("hidden"); return; }
  bar.classList.remove("hidden");
  $("#cartCount").textContent = S.cart.length;
  $("#cartLabel").textContent = "ausgewählt:";
  $("#cartNames").textContent = S.cart.map(c => c.name).join(", ");
}
$("#btnCartClear").onclick = () => { S.cart = []; renderCart(); if (S.view.tab === "add") $$(".result.selected").forEach(r => { r.classList.remove("selected"); const b = $("[data-add]", r); b.classList.remove("btn-primary"); b.textContent = "+ Auswählen"; }); };
$("#btnCartPlan").onclick = () => makePlan(S.cartTarget, S.cart.map(c => ({ source: c.source, projectId: c.projectId, versionId: c.versionId || "" })));

// ---------- plan ----------
async function makePlan(target, requests, updateAll = false) {
  const md = modal(`<div class="modal-head"><div style="flex:1"><h2>${updateAll ? "Nach Updates suchen" : "Abhängigkeiten werden geprüft"}</h2></div></div>
    <div class="modal-body">${loading(updateAll ? "Prüfe alle installierten Einträge …" : "Suche passende Versionen und Voraussetzungen …")}</div>`);
  let plan;
  try { plan = await api("/api/plan", { type: target.type, id: target.id, requests, updateAll }); }
  catch (e) {
    md.querySelector(".modal").innerHTML = `<div class="modal-head"><h2>Fehler</h2></div><div class="modal-body"><div class="banner banner-err"><span class="b-ico">✕</span><div>${esc(e.message)}</div></div></div><div class="modal-foot"><button class="btn" data-close>Schließen</button></div>`;
    bindClose(md);
    return;
  }
  closeModal(md);
  planModal(plan, target, requests, updateAll);
}

function planModal(plan, target, requests, updateAll) {
  const items = plan.items || [];
  const todo = items.filter(i => i.action !== "keep" && !i.manual);
  const chosen = todo.filter(i => i.explicit);
  const auto = todo.filter(i => !i.explicit);
  const keep = items.filter(i => i.action === "keep");
  const manual = items.filter(i => i.manual && i.action !== "keep");
  const errs = plan.errors || [], conf = plan.conflicts || [], warn = plan.warnings || [], opt = plan.optional || [];
  const noun = plan.target.kind === "plugin" ? "Plugins" : "Mods";

  const row = i => `<div class="row plan-row">${iconHTML(i.iconUrl, i.name)}
    <div class="grow"><div class="row-title">${esc(i.name)}
      ${i.action === "update" ? `<span class="pill pill-gold">Update</span>` : ""}
      ${i.version?.type && i.version.type !== "release" ? `<span class="pill pill-gold">${esc(i.version.type)}</span>` : ""}</div>
      <div class="row-meta">${i.action === "update" ? `<span class="mono">${esc(i.fromVersion)}</span> → ` : ""}<span class="mono">${esc(i.version?.number || "")}</span>
      ${!i.explicit && (i.requiredBy || []).length ? ` · benötigt von <b>${esc(i.requiredBy.join(", "))}</b>` : ""}${i.note ? " · " + esc(i.note) : ""}</div></div></div>`;

  const group = (title, list, extra = "") => list.length ? `<div class="plan-group"><div class="section-label" style="margin:0 0 6px">${title} · ${list.length}</div>${extra}<div class="list">${list.map(row).join("")}</div></div>` : "";
  const banners = [
    ...errs.map(e => `<div class="banner banner-err"><span class="b-ico">✕</span><div>${esc(e)}</div></div>`),
    ...conf.map(e => `<div class="banner banner-err"><span class="b-ico">⚔</span><div>${esc(e)}</div></div>`),
    ...warn.map(e => `<div class="banner banner-warn"><span class="b-ico">⚠</span><div>${esc(e)}</div></div>`),
  ].join("");

  const nothing = !todo.length && !manual.length;
  const md = modal(`<div class="modal-head"><div style="flex:1"><h2>${updateAll ? "Updates" : "Installationsplan"} für ${esc(plan.target.name)}</h2>
      <div class="sub">${nothing ? (updateAll ? "Alles ist auf dem neuesten Stand." : "Es gibt nichts Neues zu installieren.") : `${todo.length} Datei(en) werden geladen${auto.length ? `, davon ${auto.length} automatisch als Voraussetzung` : ""}.`}</div></div></div>
    <div class="modal-body">
      ${banners}
      ${group(updateAll ? "Aktualisierungen" : "Deine Auswahl", updateAll ? todo : chosen)}
      ${updateAll ? "" : group("Wird automatisch mitinstalliert (Voraussetzungen)", auto)}
      ${manual.length ? `<div class="plan-group"><div class="section-label" style="margin:0 0 6px">Manueller Download nötig · ${manual.length}</div>
        <div class="banner banner-warn" style="margin-bottom:8px"><span class="b-ico">⚠</span><div>Der Autor erlaubt keine Downloads über andere Programme. Lade die Datei auf CurseForge herunter und leg sie in den ${noun}-Ordner (Button nach der Installation).</div></div>
        <div class="list">${manual.map(row).join("")}</div></div>` : ""}
      ${opt.length ? `<div class="plan-group"><div class="section-label" style="margin:0 0 6px">Optionale Erweiterungen · ${opt.length}</div>
        <div class="list">${opt.map((o, i) => `<label class="row plan-row" style="cursor:pointer"><input type="checkbox" data-opt="${i}" style="accent-color:var(--green);width:16px;height:16px">
          ${iconHTML(o.iconUrl, o.name)}<div class="grow"><div class="row-title">${esc(o.name)}</div><div class="row-meta">optional für ${esc(o.for)}</div></div></label>`).join("")}</div>
        <div style="margin-top:8px"><button class="btn btn-sm" id="optAdd" disabled>Auswahl übernehmen &amp; neu prüfen</button></div></div>` : ""}
      ${keep.length ? `<details class="plan-group"><summary class="section-label" style="cursor:pointer;margin:0">Bereits vorhanden · ${keep.length}</summary><div class="list" style="margin-top:6px">${keep.map(row).join("")}</div></details>` : ""}
    </div>
    <div class="modal-foot">
      <span class="muted" style="font-size:12.5px">${esc(plan.target.dir)}</span><span class="spacer"></span>
      <button class="btn btn-ghost" data-close>Abbrechen</button>
      ${nothing ? `<button class="btn btn-primary" data-close>OK</button>` :
        `<button class="btn ${errs.length || conf.length ? "btn-danger" : "btn-primary"}" id="planGo">${errs.length || conf.length ? "Trotzdem installieren" : `Installieren (${todo.length})`}</button>`}
    </div>`, true);

  if ($("#optAdd", md)) {
    const boxes = $$("[data-opt]", md);
    boxes.forEach(b => b.onchange = () => { $("#optAdd", md).disabled = !boxes.some(x => x.checked); });
    $("#optAdd", md).onclick = () => {
      const extra = boxes.filter(b => b.checked).map(b => opt[+b.dataset.opt]);
      extra.forEach(o => { if (!inCart(o.source, o.projectId)) S.cart.push({ source: o.source, projectId: o.projectId, name: o.name, iconUrl: o.iconUrl }); });
      renderCart();
      closeModal(md);
      const reqs = updateAll ? extra.map(o => ({ source: o.source, projectId: o.projectId })) : S.cart.map(c => ({ source: c.source, projectId: c.projectId, versionId: c.versionId || "" }));
      makePlan(target, reqs, false);
    };
  }
  if ($("#planGo", md)) $("#planGo", md).onclick = async () => {
    try {
      const { job } = await api("/api/apply", { planId: plan.id });
      closeModal(md);
      jobModal(job, `${noun} werden installiert`, async () => {
        S.cart = [];
        renderCart();
        await refreshState();
        if (S.view.type === target.type && S.view.id === target.id) renderTarget($("#main"), target.type, target.id, "installed");
      }, plan.target.dir);
    } catch (e) { toast(e.message, true); }
  };
}

// ---------- jobs ----------
function jobModal(jobId, title, onDone, folder) {
  const md = modal(`<div class="modal-head"><div style="flex:1"><h2>${esc(title)}</h2></div></div>
    <div class="modal-body">
      <div class="progress indet" id="jProg"><div style="width:0"></div></div>
      <div class="job-step" id="jStep">Starte …</div>
      <div id="jResult"></div>
      <div class="log" id="jLog"></div>
    </div>
    <div class="modal-foot" id="jFoot"><span class="muted" style="font-size:12.5px">Du kannst das Fenster offen lassen, der Vorgang läuft weiter.</span></div>`, true, true);
  let doneCalled = false;
  const poll = async () => {
    let j;
    try { j = await api("/api/job?id=" + jobId); } catch (e) { $("#jStep", md).textContent = e.message; return; }
    const prog = $("#jProg", md);
    prog.classList.toggle("indet", j.progress < 0 && j.status === "running");
    $("div", prog).style.width = (Math.max(0, j.progress) * 100).toFixed(1) + "%";
    $("#jStep", md).textContent = j.status === "running" ? (j.step || "Läuft …") : j.status === "done" ? "Fertig." : "Fehlgeschlagen.";
    const log = $("#jLog", md);
    const atBottom = log.scrollTop + log.clientHeight >= log.scrollHeight - 20;
    log.textContent = (j.log || []).join("\n");
    if (atBottom) log.scrollTop = log.scrollHeight;
    if (j.status === "running") { setTimeout(poll, 500); return; }

    const r = j.result || {};
    let html = "";
    if (j.status === "error") html += `<div class="banner banner-err"><span class="b-ico">✕</span><div>${esc(j.error)}</div></div>`;
    if ((r.failed || []).length) html += `<div class="banner banner-err"><span class="b-ico">✕</span><div>Fehlgeschlagen: ${esc(r.failed.join(" · "))}</div></div>`;
    if ((r.manual || []).length) html += `<div class="banner banner-warn"><span class="b-ico">⚠</span><div>Bitte manuell herunterladen und in den Ordner legen:<br>${r.manual.map(m => `<a href="${esc(m.version.file.manualUrl)}" data-ext>${esc(m.name)} (${esc(m.version.file.fileName)})</a>`).join("<br>")}</div></div>`;
    if (j.status === "done" && (r.installed || r.updated)) {
      const n = (r.installed || []).length, u = (r.updated || []).length;
      html += `<div class="banner banner-info"><span class="b-ico">✓</span><div>${n ? n + " installiert" : ""}${n && u ? ", " : ""}${u ? u + " aktualisiert" : ""}${!n && !u ? "Nichts geändert" : ""}.</div></div>`;
    }
    $("#jResult", md).innerHTML = html;
    $("#jFoot", md).innerHTML = `${folder || r.dir ? `<button class="btn" id="jFolder">📁 Ordner öffnen</button>` : ""}<span class="spacer"></span>
      ${j.status === "done" ? `<button class="btn btn-play btn-sm" id="jLaunch" style="height:36px">▶ Minecraft Launcher öffnen</button>` : ""}
      <button class="btn btn-primary" data-close>Schließen</button>`;
    bindClose(md);
    if ($("#jFolder", md)) $("#jFolder", md).onclick = () => api("/api/open-folder", { path: folder || r.dir });
    if ($("#jLaunch", md)) $("#jLaunch", md).onclick = openLauncher;
    if (!doneCalled) {
      doneCalled = true;
      if (j.status === "done") { try { await onDone?.(j.result, md); } catch (e) { toast(e.message, true); } }
      else refreshState().catch(() => {});
    }
  };
  poll();
}

// ---------- modals ----------
function modal(inner, wide = false, sticky = false) {
  const back = document.createElement("div");
  back.className = "modal-back";
  back.innerHTML = `<div class="modal ${wide ? "modal-wide" : ""}" role="dialog" aria-modal="true">${inner}</div>`;
  $("#modalRoot").appendChild(back);
  if (!sticky) back.addEventListener("mousedown", e => { if (e.target === back) closeModal(back); });
  bindClose(back);
  return back;
}
function bindClose(md) { $$("[data-close]", md).forEach(b => b.onclick = () => closeModal(md)); }
function closeModal(md) { md.remove(); }
document.addEventListener("keydown", e => {
  if (e.key !== "Escape") return;
  const all = $$("#modalRoot .modal-back");
  const top = all[all.length - 1];
  if (top && $("[data-close]", top)) closeModal(top);
});

function confirmModal(title, html, okLabel, onOk) {
  const md = modal(`<div class="modal-head"><h2>${esc(title)}</h2></div><div class="modal-body">${html}</div>
    <div class="modal-foot"><button class="btn btn-ghost" data-close>Abbrechen</button><button class="btn btn-danger" id="cOk">${esc(okLabel)}</button></div>`);
  $("#cOk", md).onclick = async () => {
    $("#cOk", md).disabled = true;
    try { await onOk(); closeModal(md); } catch (e) { toast(e.message, true); $("#cOk", md).disabled = false; }
  };
}

function instanceSettingsModal(inst) {
  const md = modal(`<div class="modal-head"><h2>Instanz bearbeiten</h2></div>
    <div class="modal-body"><div class="field"><label>Name</label><input class="input" id="sName" value="${esc(inst.name)}" maxlength="60"></div>
    <div class="field" style="margin-top:14px"><label>Arbeitsspeicher</label><div class="range-row"><input type="range" id="sMem" min="0" max="16" value="${inst.memoryGB || 0}"><span class="range-val" id="sMemVal"></span></div></div>
    <div class="kv" style="margin-top:18px">
      <div>Launcher-Version</div><div class="mono">${esc(inst.versionId)}</div>
      <div>Ordner</div><div class="mono">${esc((S.state.config.instancesDir || "") + "\\" + inst.id)}</div></div>
    <div style="margin-top:14px"><button class="btn btn-sm" id="sRe">Profil im Launcher neu eintragen</button>
      <span class="muted" style="font-size:12px;margin-left:8px">falls es im Launcher fehlt</span></div></div>
    <div class="modal-foot"><button class="btn btn-ghost" data-close>Abbrechen</button><button class="btn btn-primary" id="sSave">Speichern</button></div>`);
  const lbl = () => $("#sMemVal", md).textContent = +$("#sMem", md).value === 0 ? "Standard" : $("#sMem", md).value + " GB";
  $("#sMem", md).oninput = lbl; lbl();
  $("#sRe", md).onclick = () => api("/api/instances/reprofile", { id: inst.id }).then(() => toast("Profil eingetragen. Launcher ggf. neu starten.")).catch(e => toast(e.message, true));
  $("#sSave", md).onclick = async () => {
    try {
      await api("/api/instances/settings", { id: inst.id, name: $("#sName", md).value, memoryGB: +$("#sMem", md).value });
      closeModal(md);
      await refreshState();
      render();
    } catch (e) { toast(e.message, true); }
  };
}

function deleteInstanceModal(inst) {
  confirmModal("Instanz löschen?", `<p>Das Profil „${esc(inst.name)} (CraftKit)“ wird aus dem Minecraft Launcher entfernt.</p>
    <label class="check"><input type="checkbox" id="dFiles"> Auch den Ordner mit Mods, Welten und Einstellungen löschen</label>
    <p class="muted" style="font-size:12.5px">Ohne Haken bleiben deine Welten erhalten.</p>`, "Löschen", async () => {
    await api("/api/instances/delete", { id: inst.id, deleteFiles: $("#dFiles").checked });
    await refreshState();
    go({ type: "welcome" });
  });
}

function deletePluginFolder(pf) {
  confirmModal("Plugin-Ordner entfernen?", `<p>„${esc(pf.name)}“ wird aus CraftKit entfernt. Die Plugins im Ordner bleiben erhalten.</p>`, "Entfernen", async () => {
    await api("/api/plugin-folders/delete", { id: pf.id });
    await refreshState();
    go({ type: "welcome" });
  });
}

// ---------- profiles found in the launcher ----------
async function renderFound(m, key) {
  m.innerHTML = `<div class="main-inner">${loading("Lese Profil und Mods …")}</div>`;
  let d;
  try { d = await api("/api/profile-scan?key=" + encodeURIComponent(key)); }
  catch (e) { m.innerHTML = `<div class="main-inner"><div class="banner banner-err"><span class="b-ico">✕</span><div>${esc(e.message)}</div></div></div>`; return; }
  if (S.view.id !== key) return;
  const p = d.profile, jars = d.jars || [], miss = d.missing || [];
  const ico = LOADERS[p.loader]?.ico || "◇";
  m.innerHTML = `<div class="main-inner">
    <div class="page-head"><div class="head-ico ${LOADERS[p.loader] ? "ld-" + esc(p.loader) : ""}" style="${LOADERS[p.loader] ? "" : "background:var(--panel-2)"}">${ico}</div>
      <div class="grow"><h1>${esc(p.name)}</h1><div class="head-meta">
        <span class="pill">${esc(loaderLabel(p.loader))}${p.loaderVersion ? " " + esc(p.loaderVersion) : ""}</span>
        ${p.mcVersion ? `<span class="pill">Minecraft ${esc(p.mcVersion)}</span>` : ""}
        <span class="pill mono" title="Version im Launcher">${esc(p.versionId)}</span>
        ${p.installed ? "" : `<span class="pill pill-gold" title="Die Version fehlt im versions-Ordner">Version nicht installiert</span>`}
      </div></div>
      <div class="actions"><button class="btn btn-sm" id="fFolder">📁 Ordner</button><button class="btn btn-primary" id="fAdopt">In CraftKit übernehmen</button></div>
    </div>
    <div class="banner banner-info"><span class="b-ico">ℹ</span><div>Dieses Profil kommt aus dem offiziellen Launcher. Übernimmst du es, erkennt CraftKit die vorhandenen Mods online und kann sie dann aktualisieren, Abhängigkeiten prüfen und neue Mods dazuinstallieren. Profil, Welten und Dateien bleiben wie sie sind.
      <div class="mono muted" style="margin-top:4px;font-size:12px">${esc(p.gameDir)}</div></div></div>
    ${p.loader === "optifine" ? `<div class="banner banner-warn"><span class="b-ico">⚠</span><div>OptiFine-Profile können keine Mods über einen Mod-Loader laden. Beim Übernehmen wird der Loader anhand der vorhandenen Mods erkannt.</div></div>` : ""}
    ${miss.length ? missingBanner(miss.map(x => ({ ...x, projectId: "" }))).replace("</ul>", "</ul><div class='muted' style='margin-top:6px;font-size:12.5px'>Nach dem Übernehmen kannst du fehlende Abhängigkeiten mit einem Klick installieren.</div>") : ""}
    <div class="section-label">Mods im Ordner · ${jars.length}</div>
    ${jars.length ? `<div class="card list">${jars.map(f => `<div class="row">${iconHTML("", f.name)}<div class="grow">
      <div class="row-title">${esc(f.name)}${f.disabled ? `<span class="pill">deaktiviert</span>` : ""}<span class="pill">${esc(loaderLabel(f.loader === "plugin" ? "unknown" : f.loader))}</span></div>
      <div class="row-meta">${f.version ? `<span class="mono">${esc(f.version)}</span> · ` : ""}${esc(f.file)}</div></div></div>`).join("")}</div>`
      : `<div class="card empty">Keine Mods in diesem Profil.</div>`}
  </div>`;
  $("#fFolder").onclick = () => api("/api/open-folder", { path: p.gameDir });
  $("#fAdopt").onclick = async () => {
    try {
      const { job } = await api("/api/adopt", { key });
      jobModal(job, `„${p.name}“ wird übernommen`, async (inst) => {
        await refreshState();
        if (inst && inst.id) { go({ type: "instance", id: inst.id }); toast("Übernommen – CraftKit verwaltet dieses Profil jetzt."); }
      });
    } catch (e) { toast(e.message, true); }
  };
}

// ---------- server matching ----------
let releaseCache = null;
async function releases() {
  if (!releaseCache) releaseCache = await api("/api/game-versions?loader=vanilla&snapshots=0").catch(() => []);
  return releaseCache;
}

function serverCard(info) {
  const fav = info.favicon && info.favicon.startsWith("data:image/") ? `<img src="${esc(info.favicon)}" style="width:48px;height:48px;border-radius:6px;image-rendering:pixelated" alt="">` : `<div class="mod-icon" style="width:48px;height:48px">🌐</div>`;
  return `<div class="card" style="display:flex;gap:14px;padding:14px;margin-top:14px;align-items:flex-start">${fav}
    <div style="flex:1;min-width:0">
      <div class="row-title">${esc(info.host)}${info.port !== 25565 ? ":" + info.port : ""} <span class="pill pill-green"><span class="dot"></span>online · ${info.pingMs} ms</span></div>
      ${info.motd ? `<div class="muted" style="white-space:pre-wrap;margin:4px 0">${esc(info.motd)}</div>` : ""}
      <div class="head-meta" style="margin-top:6px">
        <span class="pill">Version: ${esc(info.versionName || "?")}</span>
        <span class="pill">${info.playersOnline}/${info.playersMax} Spieler</span>
        ${info.software ? `<span class="pill">${esc(info.software)}</span>` : ""}
        ${info.loader ? `<span class="pill pill-blue">${esc(LOADERS[info.loader]?.name || info.loader)}-Server</span>` : ""}
        ${info.mods?.length ? `<span class="pill pill-blue">${info.mods.length} Mods nötig</span>` : ""}
      </div>
      ${info.multiVersion ? `<div class="muted" style="font-size:12.5px;margin-top:6px">Der Server akzeptiert mehrere Minecraft-Versionen – die vorgeschlagene ist seine eigentliche Version.</div>` : ""}
    </div></div>`;
}

async function serverModal(inst) {
  const md = modal(`<div class="modal-head"><div style="flex:1"><h2>🌐 Mit Server abgleichen</h2>
      <div class="sub">${inst ? `Prüft, ob „${esc(inst.name)}“ zum Server passt, und passt die Installation bei Bedarf an.` : "Richtet eine Installation ein, die genau zum Server passt."}</div></div></div>
    <div class="modal-body">
      <div class="field"><label>Server-Adresse</label>
        <div style="display:flex;gap:8px"><input class="input" id="sAddr" style="flex:1" placeholder="z. B. play.meinserver.at oder 192.168.0.10:25565" value="${esc(inst?.serverAddress || "")}">
        <button class="btn btn-primary" id="sPing">Prüfen</button></div>
        <button class="linkish" id="sManual" style="align-self:flex-start;margin-top:4px">oder Version von Hand angeben</button></div>
      <div id="sInfo"></div>
      <div id="sCfg" class="hidden" style="margin-top:16px">
        <div class="form-grid">
          <div class="field"><label>Minecraft-Version des Servers</label><select class="input" id="sMc"></select></div>
          <div class="field"><label>Loader</label><select class="input" id="sLoader">${Object.entries(LOADERS).map(([k, l]) => `<option value="${k}">${l.name}</option>`).join("")}</select>
            <span class="hint" id="sLoaderHint"></span></div>
        </div>
        <div id="sMods"></div>
        <div id="sVerdict" style="margin-top:14px"></div>
      </div>
    </div>
    <div class="modal-foot"><button class="btn btn-ghost" data-close>Abbrechen</button><button class="btn btn-primary hidden" id="sGo"></button></div>`, true);

  let info = null;
  const showCfg = async () => {
    $("#sCfg", md).classList.remove("hidden");
    const rel = await releases();
    const cands = info?.mcCandidates || [];
    const all = [...new Set([...cands, ...rel.map(r => r.id)])];
    const def = info?.mcVersion || inst?.mcVersion || all[0];
    $("#sMc", md).innerHTML = all.map(v => `<option ${v === def ? "selected" : ""}>${esc(v)}</option>`).join("");
    let loader = info?.loader || inst?.loader || "fabric";
    $("#sLoader", md).value = LOADERS[loader] ? loader : "fabric";
    const mods = info?.mods || [];
    $("#sMods", md).innerHTML = mods.length ? `<div class="section-label">Mods, die der Server verlangt · ${mods.length}${info.modsTruncated ? " (Liste gekürzt)" : ""}</div>
      <div class="card list" style="max-height:200px;overflow:auto">${mods.map((mo, i) => `<label class="row plan-row" style="cursor:pointer"><input type="checkbox" data-smod="${i}" checked style="accent-color:var(--green);width:16px;height:16px">
        <div class="grow"><div class="row-title mono">${esc(mo.modId)}</div><div class="row-meta">${esc(mo.version || "")}</div></div></label>`).join("")}</div>
      <div class="muted" style="font-size:12.5px;margin-top:6px">Werden auf Modrinth gesucht und samt Abhängigkeiten installiert.</div>` : "";
    verdict();
  };
  const selectedMods = () => $$("[data-smod]", md).filter(b => b.checked).map(b => info.mods[+b.dataset.smod].modId);
  const verdict = () => {
    const mc = $("#sMc", md).value, loader = $("#sLoader", md).value;
    const serverLoader = info?.loader || "";
    $("#sLoaderHint", md).textContent = serverLoader
      ? (loader === serverLoader ? `Der Server nutzt ${LOADERS[serverLoader]?.name || serverLoader}.` : `Achtung: Der Server nutzt ${LOADERS[serverLoader]?.name || serverLoader}.`)
      : "Der Server verlangt keine Mods – Client-Mods wie Sodium kannst du trotzdem nutzen.";
    const go = $("#sGo", md);
    go.classList.remove("hidden");
    const nMods = inst ? Object.values(inst.mods || {}).filter(x => x.explicit).length : 0;
    const loaderOk = !serverLoader || loader === serverLoader;
    if (inst && mc === inst.mcVersion && loader === inst.loader) {
      $("#sVerdict", md).innerHTML = `<div class="banner banner-info" style="margin:0"><span class="b-ico">✓</span><div><b>Passt!</b> „${esc(inst.name)}“ läuft bereits auf Minecraft ${esc(mc)}${loaderOk ? "" : " – aber mit einem anderen Loader als der Server"}.</div></div>`;
      go.textContent = selectedMods().length ? `Server merken & ${selectedMods().length} Server-Mods installieren` : "Server merken & in Mehrspieler-Liste eintragen";
      go.onclick = linkOnly;
    } else {
      const diff = inst ? `<div class="banner banner-warn" style="margin:0"><span class="b-ico">⚠</span><div>Der Server läuft auf <b>${esc(loaderLabel(loader))} ${esc(mc)}</b>, deine Instanz auf <b>${esc(loaderLabel(inst.loader))} ${esc(inst.mcVersion)}</b>.
          CraftKit legt eine passende Instanz an, übernimmt Einstellungen, Ressourcenpakete${nMods ? ` und deine ${nMods} Mods in der jeweils passenden Version (samt Abhängigkeiten)` : ""} und trägt den Server ein. Deine bisherige Instanz bleibt unverändert.
          ${loader !== inst.loader && nMods ? `<br><span class="muted">Der Loader wechselt – es werden nur Mods übernommen, die es auch für ${esc(loaderLabel(loader))} gibt.</span>` : ""}</div></div>`
        : `<div class="banner banner-info" style="margin:0"><span class="b-ico">ℹ</span><div>CraftKit legt eine Instanz mit <b>${esc(loaderLabel(loader))} ${esc(mc)}</b> an und trägt den Server in die Mehrspieler-Liste ein.</div></div>`;
      $("#sVerdict", md).innerHTML = diff;
      go.textContent = inst ? "Passende Instanz anlegen" : "Instanz anlegen";
      go.onclick = adapt;
    }
  };
  const address = () => $("#sAddr", md).value.trim();
  const linkOnly = async () => {
    try {
      const r = await api("/api/server/link", { id: inst.id, address: address(), name: info?.host || address(), serverMods: selectedMods() });
      closeModal(md);
      toast(r.addedToList ? "Server in die Mehrspieler-Liste eingetragen." : "Server gespeichert.");
      await refreshState();
      if (r.plan && (r.plan.items?.length || r.plan.warnings?.length)) planModal(r.plan, { type: "instance", id: inst.id }, [], false);
      else render();
    } catch (e) { toast(e.message, true); }
  };
  const adapt = async () => {
    const mc = $("#sMc", md).value, loader = $("#sLoader", md).value;
    try {
      const { job } = await api("/api/server/adapt", { sourceId: inst?.id || "", address: address(), serverName: info?.host || "", mcVersion: mc, loader, serverMods: selectedMods() });
      closeModal(md);
      jobModal(job, `Instanz für ${loaderLabel(loader)} ${mc} wird angelegt`, async (res, jmd) => {
        await refreshState();
        if (res?.plan) closeModal(jmd);
        const id = res?.instance?.id;
        if (id) go({ type: "instance", id });
        if (res?.plan && (res.plan.items?.length || res.plan.errors?.length || res.plan.warnings?.length)) planModal(res.plan, { type: "instance", id }, [], false);
      });
    } catch (e) { toast(e.message, true); }
  };
  $("#sMc", md).onchange = verdict;
  $("#sLoader", md).onchange = verdict;
  md.addEventListener("change", e => { if (e.target.matches("[data-smod]")) verdict(); });
  $("#sManual", md).onclick = () => { info = null; $("#sInfo", md).innerHTML = ""; showCfg(); };
  const doPing = async () => {
    if (!address()) return toast("Bitte eine Server-Adresse eingeben.", true);
    $("#sInfo", md).innerHTML = loading("Frage den Server ab …");
    $("#sPing", md).disabled = true;
    try {
      info = await api("/api/server/ping", { address: address() });
      $("#sInfo", md).innerHTML = serverCard(info);
      showCfg();
    } catch (e) {
      info = null;
      $("#sInfo", md).innerHTML = `<div class="banner banner-err" style="margin-top:14px"><span class="b-ico">✕</span><div>${esc(e.message)}<br><span class="muted">Ist der Server offline? Du kannst die Version auch von Hand angeben.</span></div></div>`;
      showCfg();
    } finally { $("#sPing", md).disabled = false; }
  };
  $("#sPing", md).onclick = doPing;
  $("#sAddr", md).onkeydown = e => { if (e.key === "Enter") doPing(); };
  if (inst?.serverAddress) doPing(); else $("#sAddr", md).focus();
}

// ---------- move an instance to another Minecraft version ----------
function cmpMC(a, b) {
  const pa = a.split(/[.-]/).map(n => parseInt(n) || 0), pb = b.split(/[.-]/).map(n => parseInt(n) || 0);
  for (let i = 0; i < Math.max(pa.length, pb.length); i++) { if ((pa[i] || 0) !== (pb[i] || 0)) return (pa[i] || 0) - (pb[i] || 0); }
  return 0;
}

async function upgradeModal(inst) {
  const mods = Object.values(inst.mods || {}).filter(m => m.explicit);
  const md = modal(`<div class="modal-head"><div style="flex:1"><h2>⬆ Auf andere Minecraft-Version wechseln</h2>
      <div class="sub">CraftKit legt eine neue Instanz an und übernimmt ${mods.length ? `deine ${mods.length} Mods in der jeweils passenden Version (samt Abhängigkeiten), ` : ""}Einstellungen und Ressourcenpakete. „${esc(inst.name)}“ bleibt unverändert.</div></div></div>
    <div class="modal-body">
      <div class="form-grid">
        <div class="field"><label>Neue Minecraft-Version</label><select class="input" id="uMc"><option>Lade …</option></select></div>
        <div class="field"><label>Loader</label><select class="input" id="uLoader">${Object.entries(LOADERS).map(([k, l]) => `<option value="${k}" ${k === inst.loader ? "selected" : ""}>${l.name}</option>`).join("")}</select>
          <span class="hint">Beim Wechsel des Loaders werden nur Mods übernommen, die es auch dafür gibt.</span></div>
      </div>
      <label class="check" style="margin-top:14px"><input type="checkbox" id="uSaves"> Welten mitnehmen (als Kopie)</label>
      <div id="uWarn" style="margin-top:12px"></div>
    </div>
    <div class="modal-foot"><button class="btn btn-ghost" data-close>Abbrechen</button><button class="btn btn-primary" id="uGo" disabled>Neue Instanz anlegen</button></div>`, true);
  const loadVersions = async () => {
    const loader = $("#uLoader", md).value;
    $("#uMc", md).innerHTML = `<option>Lade …</option>`;
    $("#uGo", md).disabled = true;
    try {
      const list = await api(`/api/game-versions?loader=${loader}&snapshots=${S.state.config.showSnapshots ? 1 : 0}`);
      const newer = list.filter(v => cmpMC(v.id, inst.mcVersion) > 0);
      const def = (newer[0] || list[0]).id;
      $("#uMc", md).innerHTML = list.map(v => `<option value="${esc(v.id)}" ${v.id === def ? "selected" : ""}>${esc(v.id)}${v.id === inst.mcVersion ? "  (aktuell)" : ""}</option>`).join("");
      $("#uGo", md).disabled = false;
      warn();
    } catch (e) { $("#uWarn", md).innerHTML = `<div class="banner banner-err" style="margin:0"><span class="b-ico">✕</span><div>${esc(e.message)}</div></div>`; }
  };
  const warn = () => {
    const mc = $("#uMc", md).value, same = mc === inst.mcVersion && $("#uLoader", md).value === inst.loader;
    const older = cmpMC(mc, inst.mcVersion) < 0;
    let h = "";
    if (same) h += `<div class="banner banner-info" style="margin:0 0 8px"><span class="b-ico">ℹ</span><div>Das ist die aktuelle Version – es entsteht eine Kopie der Instanz.</div></div>`;
    if (older) h += `<div class="banner banner-warn" style="margin:0 0 8px"><span class="b-ico">⚠</span><div>Das ist eine <b>ältere</b> Version. Welten aus neueren Versionen können darin beschädigt werden – nimm sie besser nicht mit.</div></div>`;
    if ($("#uSaves", md).checked) h += `<div class="banner banner-info" style="margin:0"><span class="b-ico">ℹ</span><div>Die Welten werden kopiert. Sobald du eine Kopie in der neuen Version öffnest, wird sie umgewandelt – die Originale in „${esc(inst.name)}“ bleiben unberührt.</div></div>`;
    $("#uWarn", md).innerHTML = h;
  };
  $("#uLoader", md).onchange = loadVersions;
  $("#uMc", md).onchange = warn;
  $("#uSaves", md).onchange = warn;
  $("#uGo", md).onclick = async () => {
    const mc = $("#uMc", md).value, loader = $("#uLoader", md).value;
    try {
      const { job } = await api("/api/server/adapt", { sourceId: inst.id, mcVersion: mc, loader, copySaves: $("#uSaves", md).checked });
      closeModal(md);
      jobModal(job, `Instanz für ${loaderLabel(loader)} ${mc} wird angelegt`, async (res, jmd) => {
        await refreshState();
        if (res?.plan) closeModal(jmd);
        const id = res?.instance?.id;
        if (id) go({ type: "instance", id });
        if (res?.plan && (res.plan.items?.length || res.plan.errors?.length || res.plan.warnings?.length)) planModal(res.plan, { type: "instance", id }, [], false);
      });
    } catch (e) { toast(e.message, true); }
  };
  loadVersions();
}

async function checkServerPill(inst) {
  const pill = $("#srvPill");
  if (!pill) return;
  pill.onclick = () => serverModal(inst);
  try {
    const info = await api("/api/server/ping", { address: inst.serverAddress });
    if (!document.body.contains(pill)) return;
    const ok = info.mcVersion === inst.mcVersion && (!info.loader || info.loader === inst.loader);
    pill.className = "pill " + (ok ? "pill-green" : "pill-red");
    pill.innerHTML = ok ? `<span class="dot"></span>${esc(inst.serverAddress)} · passt` : `<span class="dot"></span>${esc(inst.serverAddress)} · Server hat ${esc(loaderLabel(info.loader || inst.loader))} ${esc(info.mcVersion)} – anpassen`;
  } catch {
    if (!document.body.contains(pill)) return;
    pill.className = "pill";
    pill.innerHTML = `🌐 ${esc(inst.serverAddress)} · nicht erreichbar`;
  }
}

// ---------- settings ----------
function renderSettings(m) {
  const c = S.state.config;
  m.innerHTML = `<div class="main-inner">
    <div class="page-head"><div class="grow"><h1>Einstellungen</h1><div class="sub">CraftKit ${esc(S.state.version)} · Daten in <span class="mono">${esc(S.state.dataDir)}</span></div></div></div>
    <div class="card card-pad"><div class="form-grid">
      <div class="field" style="grid-column:1/-1"><label>CurseForge-API-Key</label><input class="input mono" id="cfKey" type="password" value="${esc(c.curseforgeKey)}" autocomplete="off">
        <span class="hint">Nötig für Suche und Downloads über CurseForge. Einen eigenen Key gibt es kostenlos auf <a href="https://console.curseforge.com/" data-ext>console.curseforge.com</a>.</span></div>
      <div class="field"><label>Minecraft-Ordner</label><div style="display:flex;gap:8px"><input class="input mono" id="mcDir" style="flex:1" value="${esc(c.minecraftDir)}"><button class="btn" data-pick="mcDir">…</button></div>
        <span class="hint">Hier liegen Versionen und launcher_profiles.json.</span></div>
      <div class="field"><label>Ordner für Instanzen</label><div style="display:flex;gap:8px"><input class="input mono" id="instDir" style="flex:1" value="${esc(c.instancesDir)}"><button class="btn" data-pick="instDir">…</button></div>
        <span class="hint">Pro Instanz ein Unterordner mit mods, saves, config …</span></div>
      <div class="field"><label>Minecraft Launcher (optional)</label><input class="input mono" id="lPath" value="${esc(c.launcherPath)}" placeholder="automatisch erkennen">
        <span class="hint">Erkannt: ${S.state.launcherLabel ? esc(S.state.launcherLabel) : "nichts gefunden"}. Pfad zur MinecraftLauncher.exe nur angeben, wenn der Button nicht funktioniert.</span></div>
      <div class="field"><label>Java für Forge/NeoForge-Installer (optional)</label><input class="input mono" id="jPath" value="${esc(c.javaPath)}" placeholder="automatisch">
        <span class="hint">${S.state.java ? "Gefunden: " + esc(S.state.java) : "Kein Java gefunden – wird bei Bedarf automatisch geladen."}</span></div>
      <label class="check"><input type="checkbox" id="snap" ${c.showSnapshots ? "checked" : ""}> Snapshots standardmäßig anzeigen</label>
    </div>
    <div style="display:flex;justify-content:flex-end;margin-top:16px"><button class="btn btn-primary" id="cSave">Speichern</button></div></div></div>`;
  $$("[data-pick]").forEach(b => b.onclick = async () => {
    try { const r = await api("/api/pick-folder", { title: "Ordner wählen" }); if (r.path) $("#" + b.dataset.pick).value = r.path; }
    catch (e) { toast(e.message, true); }
  });
  $("#cSave").onclick = async () => {
    try {
      await api("/api/config", { minecraftDir: $("#mcDir").value, instancesDir: $("#instDir").value, launcherPath: $("#lPath").value,
        curseforgeKey: $("#cfKey").value, javaPath: $("#jPath").value, showSnapshots: $("#snap").checked });
      await refreshState();
      toast("Gespeichert.");
      renderSettings(m);
    } catch (e) { toast(e.message, true); }
  };
}

// ---------- launcher ----------
async function openLauncher() {
  try {
    await api("/api/open-launcher", {});
    toast("Minecraft Launcher wird geöffnet – wähle dort das Profil mit „(CraftKit)“.");
  } catch (e) { toast("Launcher konnte nicht geöffnet werden: " + e.message, true); }
}

// ---------- boot ----------
$("#btnLauncher").onclick = openLauncher;
$("#btnNewInstance").onclick = () => go({ type: "new" });
$("#btnNewPluginFolder").onclick = () => go({ type: "newPlugins" });
$("#btnSettings").onclick = () => go({ type: "settings" });
setInterval(() => refreshState().catch(() => {}), 30000);
refreshState().then(() => go({ type: "welcome" })).catch(e => {
  $("#main").innerHTML = `<div class="main-inner"><div class="banner banner-err"><span class="b-ico">✕</span><div>${esc(e.message)}</div></div></div>`;
});
