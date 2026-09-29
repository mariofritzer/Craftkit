"use strict";

const TOKEN = document.querySelector('meta[name="craftkit-token"]').content;

// ---------- translations ----------
// German text is the key; {0}, {1} … are placeholders for inserted values.
const LANGS = {
  de: "Deutsch", en: "English", es: "Español", fr: "Français", it: "Italiano", pl: "Polski", "pt-BR": "Português (Brasil)",
  nl: "Nederlands", tr: "Türkçe", ru: "Русский", uk: "Українська", "zh-CN": "简体中文", ja: "日本語", ko: "한국어",
};
let LANG = "de", DICT = {};
function tr(key, ...args) {
  const s = DICT[key] ?? key;
  return args.length ? s.replace(/\{(\d+)\}/g, (m, i) => (args[+i] ?? m)) : s;
}
// trh escapes the translated text but inserts the (already safe) HTML arguments as they are.
function trh(key, ...html) {
  return esc(DICT[key] ?? key).replace(/\{(\d+)\}/g, (m, i) => (html[+i] ?? m));
}
function detectLang(pref) {
  if (pref && LANGS[pref]) return pref;
  for (const l of navigator.languages || [navigator.language || "de"]) {
    if (LANGS[l]) return l;
    if (/^pt/i.test(l)) return "pt-BR";
    if (/^zh/i.test(l)) return "zh-CN";
    const base = l.split("-")[0];
    if (LANGS[base]) return base;
  }
  return "en";
}
async function loadLang(lang) {
  LANG = lang;
  DICT = {};
  if (lang !== "de") {
    try { DICT = await (await fetch(`i18n/${lang}.json`, { cache: "no-store" })).json(); } catch { DICT = {}; }
  }
  document.documentElement.lang = lang;
  // static texts in index.html
  $$("[data-i18n]").forEach(el => { el.textContent = tr(el.dataset.i18n); });
  $$("[data-i18n-title]").forEach(el => { el.title = tr(el.dataset.i18nTitle); });
}
function locale() { return { de: "de-AT", en: "en-GB", "pt-BR": "pt-BR", "zh-CN": "zh-CN" }[LANG] || LANG; }

const LOADERS = {
  vanilla:  { name: "Vanilla",  ico: "🌱", get desc() { return tr("Unverändertes Minecraft, keine Mods."); } },
  fabric:   { name: "Fabric",   ico: "🧵", get desc() { return tr("Leicht und schnell, riesige Auswahl für neue Versionen."); } },
  forge:    { name: "Forge",    ico: "⚒",  get desc() { return tr("Der Klassiker, viele große Mods und Modpacks."); } },
  neoforge: { name: "NeoForge", ico: "🔥", get desc() { return tr("Moderner Forge-Nachfolger ab Minecraft 1.20.2."); } },
  quilt:    { name: "Quilt",    ico: "🧶", get desc() { return tr("Fabric-Ableger, lädt auch die meisten Fabric-Mods."); } },
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

const KIND_NOUN = { get mod() { return tr("Mods"); }, get plugin() { return tr("Plugins"); }, get resourcepack() { return tr("Ressourcenpakete"); }, get shader() { return tr("Shader"); } };
const KIND_EXAMPLE = { mod: "Sodium, JEI, Create", plugin: "EssentialsX, LuckPerms", resourcepack: "Faithful, Fresh Animations", shader: "Complementary, BSL, Solas" };

// ---------- helpers ----------
const $ = (sel, root = document) => root.querySelector(sel);
const $$ = (sel, root = document) => [...root.querySelectorAll(sel)];
function esc(s) {
  return String(s ?? "").replace(/[&<>"']/g, c => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" }[c]));
}
async function api(path, body) {
  const opts = { headers: { "X-CraftKit-Token": TOKEN, "X-CraftKit-Lang": LANG } };
  if (body !== undefined) {
    opts.method = "POST";
    opts.headers["Content-Type"] = "application/json";
    opts.body = JSON.stringify(body);
  }
  let res;
  try { res = await fetch(path, opts); }
  catch (e) { throw new Error(tr("CraftKit ist nicht mehr erreichbar – bitte neu starten.")); }
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
  try { return new Intl.NumberFormat(locale(), { notation: "compact", maximumFractionDigits: 1 }).format(n); }
  catch { return String(n); }
}
function fmtDate(s) {
  if (!s) return "";
  const d = new Date(s);
  return isNaN(d) ? "" : d.toLocaleDateString(locale(), { day: "2-digit", month: "2-digit", year: "numeric" });
}
function iconHTML(url, name, cls = "mod-icon") {
  if (url) return `<img class="${cls}" src="${esc(url)}" alt="" loading="lazy" referrerpolicy="no-referrer" onerror="this.replaceWith(Object.assign(document.createElement('div'),{className:'${cls}',textContent:'${esc((name || "?")[0]).toUpperCase()}'}))">`;
  return `<div class="${cls}">${esc((name || "?")[0].toUpperCase())}</div>`;
}
function loading(text) { return `<div class="loading"><span class="spinner"></span>${esc(text || tr("Lade …"))}</div>`; }
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
  const [st, disc, upd] = await Promise.all([api("/api/state"), api("/api/discover").catch(() => ({ profiles: [], versions: [] })), api("/api/updates").catch(() => null)]);
  S.state = st;
  S.discover = disc;
  if (upd) S.updates = upd;
  renderSidebar();
  renderTopStatus();
}

function renderTopStatus() {
  const st = S.state;
  const parts = [];
  if (st.launcherRunning) parts.push(`<span class="pill pill-gold" title="${esc(tr("Neue Profile erscheinen erst nach einem Neustart des Launchers"))}"><span class="dot"></span>${esc(tr("Launcher läuft"))}</span>`);
  if (S.update?.available) parts.push(`<button class="pill pill-green" id="updPill" style="border:none;cursor:pointer" title="${esc(tr("Update installieren"))}">⬆ ${esc(tr("Update {0}", S.update.latest))}</button>`);
  if (!st.launcherLabel) parts.push(`<span class="pill pill-red" title="${esc(tr("Pfad in den Einstellungen angeben"))}"><span class="dot"></span>${esc(tr("Launcher nicht gefunden"))}</span>`);
  $("#topStatus").innerHTML = parts.join("");
  if ($("#updPill")) $("#updPill").onclick = updateModal;
}

function renderSidebar() {
  const st = S.state;
  const v = S.view;
  const insts = st.instances || [];
  $("#instanceList").innerHTML = insts.length ? insts.map(i => `
    <button class="side-item ${v.type === "instance" && v.id === i.id ? "active" : ""}" data-inst="${esc(i.id)}">
      <span class="side-ico ld-${esc(i.loader)}">${LOADERS[i.loader]?.ico || "?"}</span>
      <span class="side-text"><div class="side-title">${esc(i.name)}</div>
      <div class="side-meta">${esc(LOADERS[i.loader]?.name)} · ${esc(i.mcVersion)}${i.loader !== "vanilla" ? " · " + esc(tr("{0} Mods", i.modCount)) : ""}</div></span>
      ${updBadge("instance:" + i.id)}
    </button>`).join("") : `<div class="side-empty">${esc(tr("Noch keine Instanz. Klick auf +."))}</div>`;
  const pfs = st.pluginFolders || [];
  $("#pluginList").innerHTML = pfs.length ? pfs.map(p => `
    <button class="side-item ${v.type === "plugins" && v.id === p.id ? "active" : ""}" data-pf="${esc(p.id)}">
      <span class="side-ico ld-plugin">🔌</span>
      <span class="side-text"><div class="side-title">${esc(p.name)}</div>
      <div class="side-meta">${esc(PLATFORMS[p.platform] || p.platform)}${p.mcVersion ? " · " + esc(p.mcVersion) : ""} · ${esc(tr("{0} Plugins", p.count))}</div></span>
      ${updBadge("plugins:" + p.id)}
    </button>`).join("") : `<div class="side-empty">${esc(tr("Kein Plugin-Ordner. Klick auf +."))}</div>`;
  const found = (S.discover?.profiles || []).filter(p => !p.instanceId);
  $("#foundSection").classList.toggle("hidden", !found.length);
  $("#foundList").innerHTML = found.map(p => `
    <button class="side-item ${v.type === "found" && v.id === p.key ? "active" : ""}" data-found="${esc(p.key)}">
      <span class="side-ico ${LOADERS[p.loader] ? "ld-" + esc(p.loader) : ""}">${LOADERS[p.loader]?.ico || "◇"}</span>
      <span class="side-text"><div class="side-title">${esc(p.name)}</div>
      <div class="side-meta">${esc(loaderLabel(p.loader))}${p.mcVersion ? " · " + esc(p.mcVersion) : ""}${p.modCount ? " · " + esc(tr("{0} Mods", p.modCount)) : ""}</div></span>
    </button>`).join("");
  $$("[data-found]").forEach(b => b.onclick = () => go({ type: "found", id: b.dataset.found }));
  $$("[data-inst]").forEach(b => b.onclick = () => go({ type: "instance", id: b.dataset.inst }));
  $$("[data-pf]").forEach(b => b.onclick = () => go({ type: "plugins", id: b.dataset.pf }));
  $("#btnSettings").classList.toggle("active", v.type === "settings");
}

function updBadge(key) {
  const u = S.updates?.targets?.[key];
  return u && u.count ? `<span class="upd-badge" title="${esc(tr("{0} Update(s) verfügbar", u.count))}">↑${u.count}</span>` : "";
}

function loaderLabel(l) {
  return LOADERS[l]?.name || (l === "optifine" ? "OptiFine" : tr("Unbekannt"));
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
    ${!st.minecraftFound ? `<div class="banner banner-warn"><span class="b-ico">⚠</span><div>${tr("Kein Minecraft-Ordner unter {0} gefunden. Starte den offiziellen Launcher einmal oder passe den Pfad in den Einstellungen an.", `<span class="mono">${esc(st.config.minecraftDir)}</span>`)}</div></div>` : ""}
    <div class="hero">
      <h1>${esc(tr("Minecraft einrichten, ohne Dateien zu schieben."))}</h1>
      <p>${esc(tr("CraftKit installiert Minecraft-Versionen mit Forge, NeoForge, Fabric oder Quilt, lädt Mods und Plugins von Modrinth und CurseForge und nimmt alle Voraussetzungen automatisch mit. Gespielt wird wie gewohnt im offiziellen Launcher."))}</p>
      <div style="display:flex;gap:10px;margin-top:18px;flex-wrap:wrap">
        <button class="btn btn-primary" id="wNew">+ ${esc(tr("Neue Instanz anlegen"))}</button>
        <button class="btn" id="wPack">📦 ${esc(tr("Modpack installieren"))}</button>
        <button class="btn" id="wSrv">🌐 ${esc(tr("Für einen Server einrichten"))}</button>
        <button class="btn" id="wPf">${esc(tr("Plugin-Ordner hinzufügen"))}</button>
      </div>
      <div class="feature-grid">
        <div class="feature"><b>1 · ${esc(tr("Version wählen"))}</b><span>${esc(tr("Loader und Minecraft-Version aussuchen, CraftKit installiert alles und legt ein eigenes Profil an."))}</span></div>
        <div class="feature"><b>2 · ${esc(tr("Mods aussuchen"))}</b><span>${esc(tr("Suchen, in den Korb legen, Abhängigkeiten werden vor der Installation angezeigt."))}</span></div>
        <div class="feature"><b>3 · ${esc(tr("Spielen"))}</b><span>${esc(tr("„Minecraft Launcher öffnen“ drücken und dort das Profil der Instanz starten – neue Instanzen erkennst du an „(CraftKit)“ im Namen, übernommene behalten ihren Namen."))}</span></div>
      </div>
    </div></div>`;
  $("#wNew").onclick = () => go({ type: "new" });
  $("#wPf").onclick = () => go({ type: "newPlugins" });
  $("#wSrv").onclick = () => serverModal(null);
  $("#wPack").onclick = () => go({ type: "new", mode: "pack" });
}

// ---------- new instance wizard ----------
const W = { loader: "fabric", mc: "", lv: "", name: "", memory: 4, snapshots: false, mcList: null, lvList: null, err: "" };

async function renderNewInstance(m) {
  if (S.view.mode === "pack") return renderModpacks(m);
  W.snapshots = S.state.config.showSnapshots;
  m.innerHTML = `<div class="main-inner">
    <div class="page-head"><div class="grow"><h1>${esc(tr("Neue Instanz"))}</h1>
      <div class="sub">${esc(tr("Jede Instanz hat einen eigenen Ordner für Mods, Welten und Einstellungen und erscheint als eigenes Profil im Minecraft Launcher."))}</div></div></div>
    ${newTabs("new")}
    ${S.state.launcherRunning ? `<div class="banner banner-warn"><span class="b-ico">⚠</span><div>${esc(tr("Der Minecraft Launcher ist gerade offen. Schließ ihn am besten vorher, sonst taucht das neue Profil erst nach einem Neustart des Launchers auf."))}</div></div>` : ""}
    <div class="step-title done"><span class="step-num">1</span>${esc(tr("Loader"))}</div>
    <div class="loader-grid" id="wLoaders"></div>
    <div class="step-title"><span class="step-num">2</span>${esc(tr("Version"))}</div>
    <div class="card card-pad"><div class="form-grid">
      <div class="field"><label>${esc(tr("Minecraft-Version"))}</label><select class="input" id="wMc"></select>
        <label class="check" style="margin-top:4px"><input type="checkbox" id="wSnap"> ${esc(tr("Snapshots & ältere Typen anzeigen"))}</label></div>
      <div class="field" id="wLvField"><label id="wLvLabel">${esc(tr("Loader-Version"))}</label><select class="input" id="wLv"></select><span class="hint" id="wLvHint"></span></div>
      <div class="field"><label>${esc(tr("Name"))}</label><input class="input" id="wName" maxlength="60" placeholder="${esc(tr("z. B. Fabric 1.21 mit Freunden"))}"></div>
      <div class="field"><label>${esc(tr("Arbeitsspeicher (RAM)"))}</label>
        <div class="range-row"><input type="range" id="wMem" min="0" max="16" step="1"><span class="range-val" id="wMemVal"></span></div>
        <span class="hint">${esc(tr("„Standard“ überlässt es dem Launcher. Für größere Modpacks 6–8 GB."))}</span></div>
    </div></div>
    <div id="wErr"></div>
    <div style="display:flex;justify-content:flex-end;gap:10px;margin-top:18px">
      <button class="btn btn-ghost" id="wCancel">${esc(tr("Abbrechen"))}</button>
      <button class="btn btn-primary" id="wCreate">${esc(tr("Installieren & Profil anlegen"))}</button>
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
  const memLabel = () => $("#wMemVal").textContent = +$("#wMem").value === 0 ? tr("Standard") : $("#wMem").value + " GB";
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
  sel.innerHTML = `<option>${esc(tr("Lade Versionen …"))}</option>`;
  sel.disabled = true;
  $("#wErr").innerHTML = "";
  try {
    const list = await api(`/api/game-versions?loader=${W.loader}&snapshots=${W.snapshots ? 1 : 0}`);
    if (!list || !list.length) throw new Error(tr("Keine Versionen gefunden."));
    const prev = W.mc;
    const have = new Set((S.discover?.versions || []).filter(x => x.loader === W.loader).map(x => x.mcVersion));
    sel.innerHTML = list.map(v => `<option value="${esc(v.id)}">${esc(v.id)}${v.type !== "release" ? " (" + esc(v.type) + ")" : ""}${have.has(v.id) ? "  ✓ " + esc(tr("installiert")) : ""}</option>`).join("");
    W.mc = list.some(v => v.id === prev) ? prev : list[0].id;
    sel.value = W.mc;
    sel.disabled = false;
    suggestName();
    loadLoaderVersions();
  } catch (e) {
    sel.innerHTML = `<option>–</option>`;
    $("#wErr").innerHTML = `<div class="banner banner-err" style="margin-top:14px"><span class="b-ico">✕</span><div>${esc(tr("Versionen konnten nicht geladen werden: {0}", e.message))}</div></div>`;
  }
}

async function loadLoaderVersions() {
  const field = $("#wLvField");
  if (W.loader === "vanilla") { field.style.visibility = "hidden"; W.lv = ""; return; }
  field.style.visibility = "visible";
  $("#wLvLabel").textContent = tr("{0}-Version", LOADERS[W.loader].name);
  const sel = $("#wLv");
  sel.innerHTML = `<option>${esc(tr("Lade …"))}</option>`;
  sel.disabled = true;
  $("#wLvHint").textContent = "";
  try {
    const list = await api(`/api/loader-versions?loader=${W.loader}&mc=${encodeURIComponent(W.mc)}`);
    if (!list || !list.length) throw new Error(tr("Keine {0}-Version für {1}.", LOADERS[W.loader].name, W.mc));
    const haveLv = new Set((S.discover?.versions || []).filter(x => x.loader === W.loader && x.mcVersion === W.mc).map(x => x.loaderVersion));
    const isHave = v => haveLv.has(v.version) || (W.loader === "forge" && haveLv.has(v.version.split("-").slice(1).join("-")));
    sel.innerHTML = list.map(v => `<option value="${esc(v.version)}">${esc(v.version)}${v.recommended ? "  ★ " + esc(tr("empfohlen")) : ""}${!v.stable ? "  (Beta)" : ""}${isHave(v) ? "  ✓ " + esc(tr("installiert")) : ""}</option>`).join("");
    const rec = list.find(v => v.recommended) || list[0];
    W.lv = rec.version;
    sel.value = W.lv;
    sel.disabled = false;
    $("#wLvHint").textContent = tr("{0} Versionen verfügbar, die empfohlene ist vorausgewählt.", list.length);
  } catch (e) {
    sel.innerHTML = `<option>–</option>`;
    W.lv = "";
    $("#wLvHint").textContent = e.message;
  }
}

async function createInstanceClicked() {
  const req = { name: $("#wName").value.trim(), mcVersion: W.mc, loader: W.loader, loaderVersion: W.lv, memoryGB: W.memory };
  if (!req.mcVersion) return toast(tr("Bitte eine Minecraft-Version wählen."), true);
  if (req.loader !== "vanilla" && !req.loaderVersion) return toast(tr("Bitte eine Loader-Version wählen."), true);
  try {
    const { job } = await api("/api/instances/create", req);
    jobModal(job, tr("{0} {1} wird installiert", LOADERS[req.loader].name, req.mcVersion), async (res) => {
      await refreshState();
      if (res && res.id) {
        go({ type: "instance", id: res.id });
        toast(tr("Fertig! Das Profil heißt im Launcher „{0}“.", res.name + " (CraftKit)"));
      }
    });
  } catch (e) { toast(e.message, true); }
}

function newTabs(active) {
  setTimeout(() => $$("[data-newtab]").forEach(b => b.onclick = () => go({ type: "new", mode: b.dataset.newtab })), 0);
  return `<div class="tabs"><button class="tab ${active === "new" ? "active" : ""}" data-newtab="new">${esc(tr("Selbst zusammenstellen"))}</button>
    <button class="tab ${active === "pack" ? "active" : ""}" data-newtab="pack">📦 ${esc(tr("Modpack"))}</button></div>`;
}

// ---------- modpacks ----------
let packSource = "modrinth";
function renderModpacks(m) {
  m.innerHTML = `<div class="main-inner">
    <div class="page-head"><div class="grow"><h1>${esc(tr("Neue Instanz"))}</h1>
      <div class="sub">${esc(tr("Ein Modpack bringt Loader, Mods und Einstellungen fertig abgestimmt mit. CraftKit legt dafür eine eigene Instanz an."))}</div></div></div>
    ${newTabs("pack")}
    <div class="card card-pad" id="dropZone" style="display:flex;align-items:center;gap:16px;border-style:dashed">
      <div style="font-size:28px">📥</div>
      <div class="grow" style="flex:1"><b>${esc(tr("Modpack-Datei importieren"))}</b><div class="muted" style="font-size:13px">${esc(tr(".mrpack von Modrinth oder .zip von CurseForge – hierher ziehen oder auswählen."))}</div></div>
      <input type="file" id="packFile" accept=".mrpack,.zip" class="hidden"><button class="btn" id="packPick">${esc(tr("Datei wählen …"))}</button>
    </div>
    <div class="section-label">${esc(tr("Oder Modpack suchen"))}</div>
    <div class="searchbar">
      <div class="seg">${Object.entries(SOURCES).map(([k, n]) => `<button data-psrc="${k}" class="${packSource === k ? "active" : ""}">${n}</button>`).join("")}</div>
      <input class="input search-input" id="pq" placeholder="${esc(tr("z. B. {0} …", "Fabulously Optimized, All the Mods, Create"))}" autocomplete="off">
    </div>
    <div id="packResults"></div></div>`;
  const file = $("#packFile");
  $("#packPick").onclick = () => file.click();
  file.onchange = () => file.files[0] && uploadPack(file.files[0]);
  const dz = $("#dropZone");
  dz.ondragover = e => { e.preventDefault(); dz.style.borderColor = "var(--green)"; };
  dz.ondragleave = () => { dz.style.borderColor = ""; };
  dz.ondrop = e => { e.preventDefault(); dz.style.borderColor = ""; const f = e.dataTransfer.files[0]; if (f) uploadPack(f); };
  $$("[data-psrc]").forEach(b => b.onclick = () => { packSource = b.dataset.psrc; $$("[data-psrc]").forEach(x => x.classList.toggle("active", x === b)); searchPacks($("#pq").value, 0); });
  let timer;
  $("#pq").oninput = () => { clearTimeout(timer); timer = setTimeout(() => searchPacks($("#pq").value, 0), 350); };
  searchPacks("", 0);
}

let packSeq = 0;
async function searchPacks(q, offset) {
  const seq = ++packSeq, box = $("#packResults");
  if (!box) return;
  if (offset === 0) box.innerHTML = loading(tr("Suche …"));
  let res;
  try { res = await api(`/api/modpacks/search?source=${packSource}&q=${encodeURIComponent(q)}&offset=${offset}`); }
  catch (e) {
    if (seq !== packSeq) return;
    box.innerHTML = `<div class="banner banner-err"><span class="b-ico">✕</span><div>${esc(e.message)}</div></div>`;
    return;
  }
  if (seq !== packSeq) return;
  const hits = res.hits || [];
  if (offset === 0 && !hits.length) { box.innerHTML = `<div class="card empty">${esc(tr("Nichts gefunden."))}</div>`; return; }
  if (offset === 0) box.innerHTML = `<div class="results" id="packGrid"></div><div id="packMore" style="text-align:center;margin-top:14px"></div>`;
  const grid = $("#packGrid");
  hits.forEach(h => {
    grid.insertAdjacentHTML("beforeend", `<div class="result" data-pack="${esc(h.id)}">${iconHTML(h.iconUrl, h.name)}<div class="grow">
      <button class="result-title title-link" data-pdet>${esc(h.name)}</button><div class="result-sum">${esc(h.summary)}</div>
      <div class="result-foot"><span>⬇ ${fmtNum(h.downloads)}</span>${h.author ? `<span>· ${esc(h.author)}</span>` : ""}<span class="spacer"></span>
        <button class="linkish" data-pver>${esc(tr("Version"))}</button>${h.pageUrl ? `<a class="linkish" href="${esc(h.pageUrl)}" data-ext>${esc(tr("Seite"))}</a>` : ""}
        <button class="btn btn-sm btn-primary" data-pinst>${esc(tr("Installieren"))}</button></div></div></div>`);
    const card = grid.lastElementChild;
    $("[data-pinst]", card).onclick = () => installPack(h, "", "");
    $("[data-pdet]", card).onclick = () => detailsModal(h.source, h.id, h.name, {});
    $("[data-pver]", card).onclick = () => packVersionPicker(h);
  });
  const shown = $$(".result", grid).length;
  $("#packMore").innerHTML = shown < res.total ? `<button class="btn" id="pMoreBtn">${esc(tr("Mehr laden"))}</button>` : "";
  if ($("#pMoreBtn")) $("#pMoreBtn").onclick = () => searchPacks(q, shown);
}

async function packVersionPicker(h) {
  const md = modal(`<div class="modal-head"><h2>${esc(tr("Version von {0}", h.name))}</h2></div><div class="modal-body" id="pvBody">${loading()}</div>
    <div class="modal-foot"><button class="btn btn-ghost" data-close>${esc(tr("Abbrechen"))}</button></div>`);
  try {
    const list = await api(`/api/modpacks/versions?source=${h.source}&project=${encodeURIComponent(h.id)}`);
    $("#pvBody", md).innerHTML = (list || []).length ? `<div class="card list">${list.map((v, i) => `<div class="row" style="cursor:pointer" data-pv="${i}"><div class="grow">
      <div class="row-title"><span class="mono">${esc(v.number)}</span>${v.type !== "release" ? `<span class="pill pill-gold">${esc(v.type)}</span>` : ""}</div>
      <div class="row-meta">${fmtDate(v.date)} · Minecraft ${esc((v.gameVersions || []).join(", "))}${(v.loaders || []).length ? " · " + esc(v.loaders.join(", ")) : ""}</div></div></div>`).join("")}</div>` : `<div class="empty">${esc(tr("Keine Versionen gefunden."))}</div>`;
    $$("[data-pv]", md).forEach(r => r.onclick = () => { closeModal(md); installPack(h, list[+r.dataset.pv].id, list[+r.dataset.pv].number); });
  } catch (e) { $("#pvBody", md).innerHTML = `<div class="banner banner-err"><span class="b-ico">✕</span><div>${esc(e.message)}</div></div>`; }
}

async function installPack(h, versionId, versionLabel) {
  try {
    const { job } = await api("/api/modpacks/install", { source: h.source, projectId: h.id, versionId });
    jobModal(job, tr("Modpack „{0}“ wird installiert", h.name + (versionLabel ? " " + versionLabel : "")), packDone);
  } catch (e) { toast(e.message, true); }
}

async function uploadPack(file) {
  if (!/\.(mrpack|zip)$/i.test(file.name)) return toast(tr("Bitte eine .mrpack- oder .zip-Datei wählen."), true);
  try {
    const res = await fetch("/api/modpacks/upload?name=", { method: "POST", headers: { "X-CraftKit-Token": TOKEN, "X-CraftKit-Lang": LANG }, body: file });
    const data = await res.json();
    if (!res.ok) throw new Error(data.error || tr("Upload fehlgeschlagen"));
    jobModal(data.job, tr("Modpack „{0}“ wird importiert", file.name), packDone);
  } catch (e) { toast(e.message, true); }
}

async function packDone(res) {
  await refreshState();
  if (res?.instance?.id) {
    go({ type: "instance", id: res.instance.id });
    toast(tr("Modpack installiert – das Profil heißt im Launcher „{0}“.", res.instance.name + " (CraftKit)"));
  }
}

// ---------- plugin folder ----------
async function renderNewPluginFolder(m, edit) {
  const pf = edit || { name: "", path: "", platform: "paper", mcVersion: "" };
  m.innerHTML = `<div class="main-inner">
    <div class="page-head"><div class="grow"><h1>${esc(edit ? tr("Plugin-Ordner bearbeiten") : tr("Plugin-Ordner hinzufügen"))}</h1>
      <div class="sub">${esc(tr("Plugins laufen auf Servern (Paper, Spigot, Purpur …). Wähle den plugins-Ordner deines Servers – CraftKit legt die Plugins samt Voraussetzungen dort ab."))}</div></div></div>
    <div class="card card-pad"><div class="form-grid">
      <div class="field" style="grid-column:1/-1"><label>${esc(tr("Ordner"))}</label>
        <div style="display:flex;gap:8px"><input class="input" id="pPath" style="flex:1" placeholder="${esc(tr("z. B. {0}", "C:\\Server\\plugins"))}" value="${esc(pf.path)}" ${edit ? "disabled" : ""}>
        ${edit ? "" : `<button class="btn" id="pPick">${esc(tr("Durchsuchen …"))}</button>`}</div></div>
      <div class="field"><label>${esc(tr("Name"))}</label><input class="input" id="pName" value="${esc(pf.name)}" placeholder="${esc(tr("z. B. {0}", "Survival-Server"))}"></div>
      <div class="field"><label>${esc(tr("Server-Software"))}</label><select class="input" id="pPlat">
        ${Object.entries(PLATFORMS).map(([k, n]) => `<option value="${k}" ${pf.platform === k ? "selected" : ""}>${n}</option>`).join("")}</select></div>
      <div class="field"><label>${esc(tr("Minecraft-Version des Servers"))}</label><select class="input" id="pMc"><option value="">${esc(tr("beliebig"))}</option></select>
        <span class="hint">${esc(tr("Damit nur passende Plugin-Versionen gewählt werden."))}</span></div>
    </div></div>
    <div style="display:flex;justify-content:flex-end;gap:10px;margin-top:18px">
      <button class="btn btn-ghost" id="pCancel">${esc(tr("Abbrechen"))}</button>
      <button class="btn btn-primary" id="pSave">${esc(edit ? tr("Speichern") : tr("Hinzufügen"))}</button>
    </div></div>`;
  api("/api/game-versions?loader=vanilla&snapshots=0").then(list => {
    $("#pMc").innerHTML = `<option value="">${esc(tr("beliebig"))}</option>` + list.map(v => `<option ${v.id === pf.mcVersion ? "selected" : ""}>${esc(v.id)}</option>`).join("");
  }).catch(() => {});
  if (!edit) $("#pPick").onclick = async () => {
    try {
      const r = await api("/api/pick-folder", { title: tr("plugins-Ordner des Servers wählen") });
      if (r.path) {
        $("#pPath").value = r.path;
        if (!$("#pName").value) {
          const parts = r.path.split(/[\\/]/).filter(Boolean);
          const last = parts[parts.length - 1] || "";
          $("#pName").value = last.toLowerCase() === "plugins" && parts.length > 1 ? parts[parts.length - 2] : last;
        }
      }
    } catch (e) { toast(tr("Ordnerauswahl nicht möglich – bitte Pfad eintippen. ({0})", e.message), true); }
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
  if (type === "instance-rp" || type === "instance-shader") {
    const k = type === "instance-rp" ? "rp" : "shader";
    tab = tab === "add" ? k + "-add" : k;
    type = "instance";
  }
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
  const noun = KIND_NOUN[t.kind] || tr("Mods");
  const items = (data.items || []).sort((a, b) => (b.explicit - a.explicit) || a.name.localeCompare(b.name));

  const headIco = inst ? `<div class="head-ico ld-${esc(inst.loader)}">${LOADERS[inst.loader].ico}</div>` : `<div class="head-ico ld-plugin">🔌</div>`;
  const meta = inst
    ? `<span class="pill">${esc(LOADERS[inst.loader].name)} ${esc(inst.loaderVersion)}</span><span class="pill">Minecraft ${esc(inst.mcVersion)}</span>${inst.memoryGB ? `<span class="pill">${inst.memoryGB} GB RAM</span>` : ""}<span class="pill pill-green" title="${esc(tr("Name im offiziellen Launcher"))}"><span class="dot"></span>${esc(tr("Profil: {0}", inst.name + (inst.adopted ? "" : " (CraftKit)")))}</span>${inst.modpack ? `<span class="pill pill-blue" title="${esc(tr("Aus Modpack installiert"))}">📦 ${esc(inst.modpack.name)} ${esc(inst.modpack.version || "")}</span>` : ""}`
    + (inst.serverAddress ? `<span class="pill" id="srvPill" style="cursor:pointer" title="${esc(tr("Server prüfen"))}">🌐 ${esc(inst.serverAddress)} <span class="spinner" style="width:11px;height:11px;border-width:2px"></span></span>` : "")
    : `<span class="pill">${esc(PLATFORMS[pf?.platform] || pf?.platform)}</span><span class="pill">Minecraft ${esc(pf?.mcVersion || tr("beliebig"))}</span>${pf && !pf.exists ? `<span class="pill pill-red">${esc(tr("Ordner fehlt"))}</span>` : ""}`;

  m.innerHTML = `<div class="main-inner">
    <div class="page-head">${headIco}
      <div class="grow"><h1>${esc(t.name)}</h1><div class="head-meta">${meta}</div></div>
      <div class="actions">
        ${inst ? `<button class="btn btn-sm" id="tShare" title="${esc(tr("Als Modpack-Datei exportieren, um sie mit Freunden zu teilen"))}">📤 ${esc(tr("Teilen"))}</button>` : ""}
        ${inst ? `<button class="btn btn-sm" id="tUpgrade" title="${esc(tr("Neue Instanz mit anderer Minecraft-Version, Mods werden übernommen"))}">⬆ ${esc(tr("Version wechseln"))}</button><button class="btn btn-sm" id="tServer">🌐 ${esc(tr("Server"))}</button>` : ""}
        <button class="btn btn-sm" id="tFolder">📁 ${esc(tr("Ordner"))}</button>
        <button class="btn btn-sm" id="tUpdate" ${items.length ? "" : "disabled"}>↻ ${esc(tr("Alle aktualisieren"))}</button>
        <button class="btn btn-sm" id="tEdit">${esc(tr("Bearbeiten"))}</button>
        <button class="btn btn-sm btn-danger" id="tDelete">${esc(inst && !inst.adopted ? tr("Löschen") : tr("Entfernen"))}</button>
      </div>
    </div>
    <div class="tabs">
      <button class="tab ${S.view.tab === "installed" ? "active" : ""}" data-tab="installed">${noun}<span class="count">${items.length}</span></button>
      <button class="tab ${S.view.tab === "add" ? "active" : ""}" data-tab="add">+ ${esc(tr("{0} hinzufügen", noun))}</button>
      ${inst ? `<button class="tab ${S.view.tab.startsWith("rp") ? "active" : ""}" data-tab="rp">🎨 ${esc(tr("Ressourcenpakete"))}</button>
        <button class="tab ${S.view.tab.startsWith("shader") ? "active" : ""}" data-tab="shader">✨ ${esc(tr("Shader"))}</button>
        <button class="tab ${S.view.tab === "worlds" ? "active" : ""}" data-tab="worlds">🌍 ${esc(tr("Welten"))}</button>
        <button class="tab ${S.view.tab === "crash" ? "active" : ""}" data-tab="crash">🩺 ${esc(tr("Absturzhilfe"))}</button>` : ""}
    </div>
    <div id="tabBody"></div></div>`;

  $$("[data-tab]").forEach(b => b.onclick = () => renderTarget(m, type, id, b.dataset.tab));
  $("#tFolder").onclick = () => api("/api/open-folder", { path: inst ? inst.dir : t.dir }).catch(e => toast(e.message, true));
  if (inst) $("#tServer").onclick = () => serverModal(inst);
  if (inst) $("#tUpgrade").onclick = () => upgradeModal(inst);
  if (inst) $("#tShare").onclick = () => shareModal(inst);
  if (inst && inst.serverAddress) checkServerPill(inst);
  $("#tUpdate").onclick = () => makePlan({ type, id }, [], true);
  $("#tEdit").onclick = () => inst ? instanceSettingsModal(inst) : renderNewPluginFolder(m, pf);
  $("#tDelete").onclick = () => inst ? deleteInstanceModal(inst) : deletePluginFolder(pf);

  const body = $("#tabBody");
  if (S.view.tab === "installed") renderInstalled(body, t, items, data.foreignInfo || []);
  else if (S.view.tab === "worlds" && inst) renderWorlds(body, inst);
  else if (S.view.tab === "crash" && inst) renderCrash(body, inst);
  else if (inst && /^(rp|shader)(-add)?$/.test(S.view.tab)) renderPacks(body, inst, S.view.tab);
  else renderSearch(body, t);
}

function renderVanilla(m, inst) {
  m.innerHTML = `<div class="main-inner">
    <div class="page-head"><div class="head-ico ld-vanilla">🌱</div>
      <div class="grow"><h1>${esc(inst.name)}</h1><div class="head-meta"><span class="pill">Vanilla</span><span class="pill">Minecraft ${esc(inst.mcVersion)}</span><span class="pill pill-green"><span class="dot"></span>${esc(tr("Profil: {0}", inst.name + (inst.adopted ? "" : " (CraftKit)")))}</span></div></div>
      <div class="actions"><button class="btn btn-sm" id="vUp">⬆ ${esc(tr("Version wechseln"))}</button><button class="btn btn-sm" id="vSrv">🌐 ${esc(tr("Server"))}</button><button class="btn btn-sm" id="vEdit">${esc(tr("Bearbeiten"))}</button><button class="btn btn-sm btn-danger" id="vDel">${esc(inst.adopted ? tr("Entfernen") : tr("Löschen"))}</button></div></div>
    <div class="banner banner-info"><span class="b-ico">ℹ</span><div>${esc(tr("Vanilla-Instanzen laden keine Mods. Wenn du Mods willst, leg eine neue Instanz mit Fabric, Forge, NeoForge oder Quilt an. Die Spieldateien lädt der Minecraft Launcher beim ersten Start."))}</div></div>
    <button class="btn btn-primary" id="vNew">+ ${esc(tr("Neue Instanz mit Mod-Loader"))}</button></div>`;
  $("#vEdit").onclick = () => instanceSettingsModal(inst);
  $("#vSrv").onclick = () => serverModal(inst);
  $("#vUp").onclick = () => upgradeModal(inst);
  $("#vDel").onclick = () => deleteInstanceModal(inst);
  $("#vNew").onclick = () => { W.mc = inst.mcVersion; go({ type: "new" }); };
}

function renderInstalled(body, t, items, foreign) {
  const noun = KIND_NOUN[t.kind] || tr("Mods");
  if (!items.length && !foreign.length) {
    body.innerHTML = `<div class="card empty"><div class="big">📦</div><div>${esc(tr("Noch keine {0} installiert.", noun))}</div>
      <div style="margin-top:14px"><button class="btn btn-primary" id="goAdd">${esc(tr("{0} suchen", noun))}</button></div></div>`;
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
        <div class="row-title">${it.source ? `<button class="title-link" data-idet="${esc(it.key)}" title="${esc(tr("Details anzeigen"))}">${esc(it.name)}</button>` : esc(it.name)}
          ${it.disabled ? `<span class="pill">${esc(tr("deaktiviert"))}</span>` : ""}
          ${it.pinned ? `<button class="pill pin-pill" data-unpin="${esc(it.key)}" title="${esc(tr("Festgehalten – Updates überspringen diese Version. Klicken zum Lösen."))}">📌 ${esc(tr("festgehalten"))}</button>` : ""}
          ${!it.explicit ? `<span class="pill pill-blue" title="${esc(tr("Automatisch als Voraussetzung installiert"))}">${esc(tr("Abhängigkeit"))}</span>` : ""}
          <span class="pill">${esc(SOURCES[it.source])}</span></div>
        <div class="row-meta"><span class="mono">${esc(it.versionNumber)}</span> · ${esc(it.fileName)}
          ${nb.length ? " · " + esc(tr("benötigt von {0}", nb.join(", "))) : ""}${deps.length ? " · " + esc(tr("braucht {0}", deps.join(", "))) : ""}</div>
      </div>
      <div class="row-actions">
        ${it.source ? `<button class="btn btn-sm btn-ghost" data-iver="${esc(it.key)}" title="${esc(tr("Andere (z. B. ältere) Version installieren"))}">${esc(tr("Version"))}</button>` : ""}
        ${it.pageUrl ? `<a class="btn btn-sm btn-ghost" href="${esc(it.pageUrl)}" data-ext>${esc(tr("Seite"))}</a>` : ""}
        <button class="btn btn-sm btn-danger" data-remove="${esc(it.key)}">${esc(tr("Entfernen"))}</button>
      </div></div>`;
  };
  const explicit = items.filter(i => i.explicit), auto = items.filter(i => !i.explicit);
  const loaderMismatch = f => t.kind === "mod" && f.loader !== "unknown" && !t.loaders.includes(f.loader);
  body.innerHTML = `
    <div id="undoBox"></div>
    ${updBanner(t)}
    <div id="missingBox"></div>
    ${explicit.length ? `<div class="section-label" style="margin-top:0">${esc(tr("Von dir gewählt"))} · ${explicit.length}</div><div class="card list">${explicit.map(row).join("")}</div>` : ""}
    ${auto.length ? `<div class="section-label">${esc(tr("Automatisch mitinstalliert"))} · ${auto.length}</div><div class="card list">${auto.map(row).join("")}</div>` : ""}
    ${foreign.length ? `<div class="section-label" style="display:flex;align-items:center;gap:10px">${esc(tr("Nicht über CraftKit installiert"))} · ${foreign.length}
        <span style="flex:1"></span><button class="btn btn-sm" id="btnIdentify" title="${esc(tr("Sucht die Dateien per Prüfsumme auf Modrinth und CurseForge"))}">🔎 ${esc(tr("Online erkennen"))}</button></div>
      <div class="card list">${foreign.map(f => `<div class="row ${f.disabled ? "row-off" : ""}">${toggleHTML(!f.disabled, `data-tfile="${esc(f.file)}"`)}${iconHTML("", f.name)}<div class="grow">
        <div class="row-title">${esc(f.name)}${f.disabled ? `<span class="pill">${esc(tr("deaktiviert"))}</span>` : ""}${loaderMismatch(f) ? `<span class="pill pill-red" title="${esc(tr("Diese Datei ist für einen anderen Loader"))}">${esc(tr("für {0}", loaderLabel(f.loader)))}</span>` : ""}</div>
        <div class="row-meta">${f.version ? `<span class="mono">${esc(f.version)}</span> · ` : ""}${esc(f.file)}${(f.depends || []).length ? " · " + esc(tr("braucht {0}", f.depends.filter(d => !["minecraft", "java", "fabricloader", "forge", "neoforge", "quilt_loader"].includes(d)).join(", ") || "–")) : ""}</div></div>
        <button class="btn btn-sm btn-danger" data-foreign="${esc(f.file)}">${esc(tr("Löschen"))}</button></div>`).join("")}</div>
      <div class="muted" style="font-size:12.5px;margin-top:6px">${esc(tr("Erkannte Dateien werden danach wie eigene Installationen verwaltet: mit Updates und Abhängigkeitsprüfung."))}</div>` : ""}`;
  $$("[data-remove]", body).forEach(b => b.onclick = () => removeFlow(t, byKey[b.dataset.remove]));
  $$("[data-idet]", body).forEach(b => b.onclick = () => { const it = byKey[b.dataset.idet]; detailsModal(it.source, it.projectId, it.name, { installedVersion: it.versionId }); });
  $$("[data-iver]", body).forEach(b => b.onclick = () => changeVersion(t, byKey[b.dataset.iver]));
  $$("[data-unpin]", body).forEach(b => b.onclick = () => confirmModal(tr("Nicht mehr festhalten?"), `<p>${esc(tr("„{0}“ wird bei „Alle aktualisieren“ und in der Update-Übersicht wieder berücksichtigt.", byKey[b.dataset.unpin].name))}</p>`, tr("Lösen"), async () => {
    await api("/api/pin", { type: t.type, id: t.id, key: b.dataset.unpin, pinned: false });
    await refreshState();
    renderTarget($("#main"), t.type, t.id, "installed");
  }));
  $$("[data-tkey]", body).forEach(b => b.onchange = () => toggleFlow(t, { key: b.dataset.tkey, name: byKey[b.dataset.tkey].name }, b.checked, b));
  $$("[data-tfile]", body).forEach(b => b.onchange = () => toggleFlow(t, { file: b.dataset.tfile, name: b.dataset.tfile }, b.checked, b));
  $$("[data-foreign]", body).forEach(b => b.onclick = () => confirmModal(tr("Datei löschen?"), esc(tr("„{0}“ wird aus dem Ordner gelöscht.", b.dataset.foreign)), tr("Löschen"), async () => {
    await api("/api/remove-foreign", { type: t.type, id: t.id, file: b.dataset.foreign });
    renderTarget($("#main"), t.type, t.id, "installed");
  }));
  if ($("#btnIdentify")) $("#btnIdentify").onclick = async () => {
    try {
      const { job } = await api("/api/identify", { type: t.type, id: t.id });
      jobModal(job, tr("Dateien werden erkannt"), async (r) => {
        toast(tr("{0} erkannt, {1} unbekannt.", (r.recognized || []).length, (r.unknown || []).length));
        await refreshState();
        renderTarget($("#main"), t.type, t.id, "installed");
      });
    } catch (e) { toast(e.message, true); }
  };
  loadMissing(t);
  loadUndo(t);
  if ($("#updAll")) $("#updAll").onclick = () => makePlan({ type: t.type, id: t.id }, [], true);
}

function updBanner(t) {
  const u = S.updates?.targets?.[t.type + ":" + t.id];
  if (!u || !u.count) return "";
  const list = u.items.slice(0, 6).map(x => `<b>${esc(x.name)}</b> <span class="mono muted">${esc(x.from)} → ${esc(x.to)}</span>`).join(" · ");
  return `<div class="banner banner-info"><span class="b-ico">↑</span><div style="flex:1"><b>${esc(tr("{0} Update(s) verfügbar", u.count))}</b><div style="margin-top:4px;font-size:13px">${list}${u.count > 6 ? " · " + esc(tr("und {0} weitere", u.count - 6)) : ""}</div></div>
    <button class="btn btn-sm btn-primary" id="updAll">${esc(tr("Jetzt aktualisieren"))}</button></div>`;
}

async function loadUndo(t) {
  let h;
  try { h = await api(`/api/history?type=${t.type}&id=${encodeURIComponent(t.id)}`); } catch { return; }
  const box = $("#undoBox");
  if (!box || !h || !h.length) return;
  const last = h[0];
  box.innerHTML = `<div class="undo-bar"><span class="muted">${esc(tr("Letzte Änderung ({0}):", timeAgo(last.created)))}</span> <b>${esc(last.label)}</b>
    <span class="spacer"></span><button class="btn btn-sm" id="undoBtn">↶ ${esc(tr("Rückgängig"))}</button></div>`;
  $("#undoBtn").onclick = () => confirmModal(tr("Rückgängig machen?"), `<p>${esc(tr("„{0}“ wird rückgängig gemacht – die vorherigen Dateien und Versionen werden wiederhergestellt.", last.label))}</p>`, tr("Rückgängig machen"), async () => {
    const r = await api("/api/rollback", { type: t.type, id: t.id });
    toast(tr("Rückgängig gemacht: {0}", r.label));
    await refreshState();
    renderTarget($("#main"), t.type, t.id, "installed");
  });
}

function timeAgo(iso) {
  const d = new Date(iso), s = (Date.now() - d) / 1000;
  if (isNaN(s)) return "";
  if (s < 60) return tr("gerade eben");
  try {
    const rtf = new Intl.RelativeTimeFormat(locale(), { numeric: "auto" });
    if (s < 3600) return rtf.format(-Math.round(s / 60), "minute");
    if (s < 86400) return rtf.format(-Math.round(s / 3600), "hour");
    if (s < 86400 * 7) return rtf.format(-Math.round(s / 86400), "day");
  } catch {}
  return fmtDate(iso);
}

function fmtSize(b) {
  if (!b) return "0 MB";
  if (b >= 1 << 30) return (b / (1 << 30)).toLocaleString(locale(), { maximumFractionDigits: 1 }) + " GB";
  return Math.max(1, Math.round(b / (1 << 20))) + " MB";
}

// ---------- mod sets ----------
async function renderSets(t) {
  if (!S.sets) S.sets = await api("/api/sets").catch(() => []);
  const box = $("#setsBox");
  if (!box || !S.sets.length) return;
  box.innerHTML = `<div class="section-label" style="margin-top:0">${esc(tr("Mod-Sets – mit einem Klick"))}</div>
    <div class="sets">${S.sets.map(st => `<button class="set-card" data-set="${esc(st.id)}"><span class="set-ico">${st.icon}</span>
      <span><b>${esc(tr(st.name))}</b><span class="muted">${esc(tr(st.description))}</span></span></button>`).join("")}</div>
    <div class="section-label">${esc(tr("Oder einzeln suchen"))}</div>`;
  $$("[data-set]", box).forEach(b => b.onclick = async () => {
    const md = modal(`<div class="modal-head"><h2>${esc(tr("Mod-Set wird zusammengestellt"))}</h2></div><div class="modal-body">${loading(tr("Suche passende Versionen …"))}</div>`);
    try {
      const plan = await api("/api/sets/plan", { type: t.type, id: t.id, set: b.dataset.set });
      closeModal(md);
      planModal(plan, { type: t.type, id: t.id }, [], false);
    } catch (e) { closeModal(md); toast(e.message, true); }
  });
}

// ---------- resource packs & shaders ----------
async function renderPacks(body, inst, tab) {
  const kind = tab.startsWith("rp") ? "rp" : "shader";
  const type = kind === "rp" ? "instance-rp" : "instance-shader";
  const adding = tab.endsWith("-add");
  let d;
  body.innerHTML = loading();
  try { d = await api(`/api/target?type=${type}&id=${encodeURIComponent(inst.id)}`); }
  catch (e) { body.innerHTML = `<div class="banner banner-err"><span class="b-ico">✕</span><div>${esc(e.message)}</div></div>`; return; }
  const t = d.target, items = (d.items || []).sort((a, b) => a.name.localeCompare(b.name)), foreign = d.foreignPacks || [];
  const noun = KIND_NOUN[t.kind];
  const help = d.shaderHelp;
  body.innerHTML = `
    <div style="display:flex;gap:10px;align-items:center;margin-bottom:14px">
      <div class="seg"><button data-pk="${kind}" class="${adding ? "" : "active"}">${esc(tr("Installiert ({0})", items.length + foreign.length))}</button><button data-pk="${kind}-add" class="${adding ? "active" : ""}">+ ${esc(tr("Hinzufügen"))}</button></div>
      <span style="flex:1"></span><button class="btn btn-sm" id="pkFolder">📁 ${esc(tr("Ordner"))}</button></div>
    ${help && !help.supported ? `<div class="banner banner-warn"><span class="b-ico">⚠</span><div style="flex:1">${esc(help.message)}</div>
      ${help.suggestId ? `<button class="btn btn-sm btn-primary" id="shaderFix">${esc(tr("{0} installieren", help.suggest === "oculus" ? "Oculus" : "Iris"))}</button>` : ""}</div>` : ""}
    ${help && help.supported ? `<div class="muted" style="font-size:12.5px;margin-bottom:10px">${tr("Shader werden über {0} geladen. Aktivieren kannst du ein Shaderpack im Spiel unter Optionen → Grafik → Shaderpakete.", `<b>${esc(help.via)}</b>`)}</div>` : ""}
    ${kind === "rp" && !adding ? `<div class="muted" style="font-size:12.5px;margin-bottom:10px">${esc(tr("Aktivieren kannst du Ressourcenpakete im Spiel unter Optionen → Ressourcenpakete."))}</div>` : ""}
    <div id="pkBody"></div>`;
  $$("[data-pk]", body).forEach(b => b.onclick = () => renderTarget($("#main"), "instance", inst.id, b.dataset.pk));
  $("#pkFolder").onclick = () => api("/api/open-folder", { path: t.dir });
  if ($("#shaderFix")) $("#shaderFix").onclick = () => makePlan({ type: "instance", id: inst.id }, [{ source: "modrinth", projectId: help.suggestId }]);
  const pb = $("#pkBody");
  if (adding) { renderSearch(pb, t); return; }
  if (!items.length && !foreign.length) {
    pb.innerHTML = `<div class="card empty"><div class="big">${kind === "rp" ? "🎨" : "✨"}</div>${esc(tr("Noch keine {0} installiert.", noun))}<div style="margin-top:14px"><button class="btn btn-primary" id="pkAdd">${esc(tr("{0} suchen", noun))}</button></div></div>`;
    $("#pkAdd").onclick = () => renderTarget($("#main"), "instance", inst.id, kind + "-add");
    return;
  }
  pb.innerHTML = `<div id="pkManaged"></div>
    ${foreign.length ? `<div class="section-label">${esc(tr("Nicht über CraftKit installiert"))} · ${foreign.length}</div>
      <div class="card list">${foreign.map(f => `<div class="row ${f.disabled ? "row-off" : ""}">${f.isDir ? `<div style="width:34px"></div>` : toggleHTML(!f.disabled, `data-pfile="${esc(f.file)}"`)}
        ${iconHTML("", f.name)}<div class="grow"><div class="row-title">${esc(f.name)}${f.disabled ? `<span class="pill">${esc(tr("deaktiviert"))}</span>` : ""}${f.isDir ? `<span class="pill">${esc(tr("Ordner"))}</span>` : ""}</div>
        <div class="row-meta">${esc(f.file)}</div></div>
        ${f.isDir ? "" : `<button class="btn btn-sm btn-danger" data-pdel="${esc(f.file)}">${esc(tr("Löschen"))}</button>`}</div>`).join("")}</div>` : ""}`;
  if (items.length) renderInstalled($("#pkManaged"), t, items, []);
  $$("[data-pfile]", pb).forEach(b => b.onchange = () => toggleFlow(t, { file: b.dataset.pfile, name: b.dataset.pfile }, b.checked, b));
  $$("[data-pdel]", pb).forEach(b => b.onclick = () => confirmModal(tr("Datei löschen?"), esc(tr("„{0}“ wird gelöscht.", b.dataset.pdel)), tr("Löschen"), async () => {
    await api("/api/remove-foreign", { type: t.type, id: t.id, file: b.dataset.pdel });
    renderTarget($("#main"), "instance", inst.id, kind);
  }));
}

// ---------- details view ----------
const SAFE_TAGS = new Set(["P", "BR", "B", "STRONG", "I", "EM", "U", "S", "DEL", "UL", "OL", "LI", "H1", "H2", "H3", "H4", "H5", "H6", "A", "IMG",
  "CODE", "PRE", "BLOCKQUOTE", "HR", "TABLE", "THEAD", "TBODY", "TR", "TH", "TD", "DIV", "SPAN", "DETAILS", "SUMMARY", "SUP", "SUB", "CENTER", "FIGURE", "FIGCAPTION"]);

// sanitize keeps only harmless markup from untrusted descriptions.
function sanitize(html) {
  const doc = new DOMParser().parseFromString(`<div>${html}</div>`, "text/html");
  const walk = node => {
    for (const el of [...node.children]) {
      if (!SAFE_TAGS.has(el.tagName)) {
        if (["SCRIPT", "STYLE", "IFRAME", "OBJECT", "EMBED", "FORM", "INPUT", "BUTTON", "TEXTAREA", "SELECT", "LINK", "META"].includes(el.tagName)) { el.remove(); continue; }
        el.replaceWith(...el.childNodes);
        continue;
      }
      for (const a of [...el.attributes]) {
        const n = a.name.toLowerCase();
        const keep = (el.tagName === "A" && n === "href") || (el.tagName === "IMG" && ["src", "alt", "width", "height"].includes(n)) ||
          ((el.tagName === "TD" || el.tagName === "TH") && ["colspan", "rowspan"].includes(n));
        if (!keep) el.removeAttribute(a.name);
      }
      if (el.tagName === "A") {
        const h = el.getAttribute("href") || "";
        if (/^https:\/\//i.test(h)) el.setAttribute("data-ext", ""); else el.removeAttribute("href");
      }
      if (el.tagName === "IMG") {
        const src = el.getAttribute("src") || "";
        if (!/^https:\/\//i.test(src)) { el.remove(); continue; }
        el.setAttribute("loading", "lazy");
        el.setAttribute("referrerpolicy", "no-referrer");
      }
      if (el.tagName === "CENTER") el.setAttribute("style", "text-align:center");
      walk(el);
    }
  };
  walk(doc.body.firstChild);
  return doc.body.firstChild.innerHTML;
}

// markdown turns the common parts of Markdown into HTML (the result is sanitized afterwards).
function markdown(md) {
  const lines = (md || "").replace(/\r/g, "").split("\n");
  const out = [];
  let list = null, para = [], inCode = false, code = [];
  const inline = t => t
    .replace(/!\[([^\]]*)\]\(([^)\s]+)[^)]*\)/g, '<img alt="$1" src="$2">')
    .replace(/\[([^\]]+)\]\(([^)\s]+)[^)]*\)/g, '<a href="$2">$1</a>')
    .replace(/`([^`]+)`/g, "<code>$1</code>")
    .replace(/\*\*([^*]+)\*\*/g, "<b>$1</b>").replace(/__([^_]+)__/g, "<b>$1</b>")
    .replace(/(^|[^*])\*([^*\s][^*]*)\*/g, "$1<i>$2</i>")
    .replace(/(https:\/\/[^\s<"')]+)(?![^<]*>|[^<]*<\/a>)/g, '<a href="$1">$1</a>');
  const flushPara = () => { if (para.length) { out.push("<p>" + inline(para.join(" ")) + "</p>"); para = []; } };
  const flushList = () => { if (list) { out.push(`<${list.tag}>${list.items.map(i => "<li>" + inline(i) + "</li>").join("")}</${list.tag}>`); list = null; } };
  for (const raw of lines) {
    const l = raw.trimEnd();
    if (/^```/.test(l.trim())) {
      if (inCode) { out.push("<pre><code>" + esc(code.join("\n")) + "</code></pre>"); code = []; inCode = false; }
      else { flushPara(); flushList(); inCode = true; }
      continue;
    }
    if (inCode) { code.push(raw); continue; }
    let m;
    if (!l.trim()) { flushPara(); flushList(); continue; }
    if ((m = l.match(/^(#{1,6})\s+(.*)$/))) { flushPara(); flushList(); out.push(`<h${m[1].length}>${inline(m[2])}</h${m[1].length}>`); continue; }
    if (/^(\*\*\*|---|___)\s*$/.test(l.trim())) { flushPara(); flushList(); out.push("<hr>"); continue; }
    if ((m = l.match(/^\s*[-*+]\s+(.*)$/))) { flushPara(); if (!list || list.tag !== "ul") { flushList(); list = { tag: "ul", items: [] }; } list.items.push(m[1]); continue; }
    if ((m = l.match(/^\s*\d+[.)]\s+(.*)$/))) { flushPara(); if (!list || list.tag !== "ol") { flushList(); list = { tag: "ol", items: [] }; } list.items.push(m[1]); continue; }
    if ((m = l.match(/^>\s?(.*)$/))) { flushPara(); flushList(); out.push("<blockquote>" + inline(m[1]) + "</blockquote>"); continue; }
    if (/^\s*</.test(l)) { flushPara(); flushList(); out.push(l); continue; } // raw HTML line
    para.push(l.trim());
  }
  if (inCode) out.push("<pre><code>" + esc(code.join("\n")) + "</code></pre>");
  flushPara(); flushList();
  return out.join("\n");
}

function renderRich(text, fmt) {
  return sanitize(fmt === "html" ? (text || "") : markdown(text || ""));
}

async function detailsModal(source, id, name, opts = {}) {
  const md = modal(`<div class="modal-head"><div style="flex:1"><h2>${esc(name)}</h2><div class="sub">${esc(tr("Lade Details …"))}</div></div></div>
    <div class="modal-body">${loading()}</div>`, true);
  md.querySelector(".modal").classList.add("modal-details");
  let d;
  try { d = await api(`/api/details?source=${source}&project=${encodeURIComponent(id)}`); }
  catch (e) {
    md.querySelector(".modal-body").innerHTML = `<div class="banner banner-err"><span class="b-ico">✕</span><div>${esc(e.message)}</div></div>`;
    md.querySelector(".modal").insertAdjacentHTML("beforeend", `<div class="modal-foot"><button class="btn" data-close>${esc(tr("Schließen"))}</button></div>`);
    bindClose(md);
    return;
  }
  const linkNames = { issues: tr("Fehler melden"), source: tr("Quellcode"), wiki: "Wiki", discord: "Discord" };
  const sideTxt = v => ({ required: tr("nötig"), optional: tr("optional"), unsupported: tr("nicht nötig") }[v] || "");
  const gallery = d.gallery || [], versions = d.versions || [];
  md.querySelector(".modal").innerHTML = `
    <div class="modal-head">${iconHTML(d.iconUrl, d.name, "mod-icon det-icon")}
      <div style="flex:1;min-width:0"><h2>${esc(d.name)}</h2><div class="sub">${esc(d.summary || "")}</div>
        <div class="head-meta">
          <span class="pill">${esc(SOURCES[d.source])}</span><span class="pill">⬇ ${fmtNum(d.downloads)}</span>
          ${d.updated ? `<span class="pill">${esc(tr("aktualisiert {0}", timeAgo(d.updated)))}</span>` : ""}
          ${d.license ? `<span class="pill">${esc(tr("Lizenz: {0}", d.license))}</span>` : ""}
          ${d.clientSide ? `<span class="pill" title="${esc(tr("Wird auf dem Client (bei dir) gebraucht?"))}">Client: ${esc(sideTxt(d.clientSide))}</span>` : ""}
          ${d.serverSide ? `<span class="pill" title="${esc(tr("Wird auf dem Server gebraucht?"))}">Server: ${esc(sideTxt(d.serverSide))}</span>` : ""}
        </div></div></div>
    <div class="tabs" style="margin:0 20px 0"><button class="tab active" data-dt="desc">${esc(tr("Beschreibung"))}</button>
      ${gallery.length ? `<button class="tab" data-dt="gal">${esc(tr("Bilder ({0})", gallery.length))}</button>` : ""}
      <button class="tab" data-dt="ver">${esc(tr("Versionen"))}</button></div>
    <div class="modal-body det-body">
      <div data-dp="desc" class="rich">${d.body ? renderRich(d.body, d.bodyFormat) : `<p class="muted">${esc(tr("Keine Beschreibung vorhanden."))}</p>`}</div>
      <div data-dp="gal" class="hidden"><div class="gallery">${gallery.map(g => `<a href="${esc(g.url)}" data-ext><img src="${esc(g.url)}" alt="${esc(g.title || "")}" loading="lazy" referrerpolicy="no-referrer">${g.title ? `<span>${esc(g.title)}</span>` : ""}</a>`).join("")}</div></div>
      <div data-dp="ver" class="hidden">${versions.map((v, i) => `<details class="ver" ${i === 0 ? "open" : ""} data-vi="${i}">
        <summary><span class="mono">${esc(v.number)}</span>${v.type !== "release" ? ` <span class="pill pill-gold">${esc(v.type)}</span>` : ""}${opts.installedVersion === v.id ? ` <span class="pill pill-green">${esc(tr("installiert"))}</span>` : ""}
          <span class="muted"> · ${fmtDate(v.date)} · ${esc((v.gameVersions || []).slice(0, 5).join(", "))}${(v.loaders || []).length ? " · " + esc(v.loaders.join(", ")) : ""}</span></summary>
        <div class="rich ver-log">${v.changelog ? renderRich(v.changelog, v.changelogFmt) : (v.changelogFmt === "html" ? `<span class="muted">${esc(tr("Lade Änderungen …"))}</span>` : `<span class="muted">${esc(tr("Kein Änderungsprotokoll."))}</span>`)}</div></details>`).join("") || `<p class="muted">${esc(tr("Keine Versionen gefunden."))}</p>`}</div>
    </div>
    <div class="modal-foot">${Object.entries(d.links || {}).map(([k, u]) => `<a class="btn btn-sm btn-ghost" href="${esc(u)}" data-ext>${esc(linkNames[k] || k)} ↗</a>`).join("")}
      ${d.pageUrl ? `<a class="btn btn-sm btn-ghost" href="${esc(d.pageUrl)}" data-ext>${esc(tr("Projektseite"))} ↗</a>` : ""}<span class="spacer"></span>
      <button class="btn btn-ghost" data-close>${esc(tr("Schließen"))}</button>
      ${opts.toggle ? `<button class="btn btn-primary" id="detSel">${esc(opts.selected() ? "✓ " + tr("Ausgewählt") : "+ " + tr("Auswählen"))}</button>` : ""}</div>`;
  bindClose(md);
  $$("[data-dt]", md).forEach(b => b.onclick = () => {
    $$("[data-dt]", md).forEach(x => x.classList.toggle("active", x === b));
    $$("[data-dp]", md).forEach(p => p.classList.toggle("hidden", p.dataset.dp !== b.dataset.dt));
  });
  // CurseForge changelogs are loaded when a version is opened
  $$("details.ver", md).forEach(det => det.addEventListener("toggle", async () => {
    const v = versions[+det.dataset.vi];
    if (!det.open || v.changelog || v.changelogFmt !== "html" || v._loading) return;
    v._loading = true;
    try {
      const r = await api(`/api/changelog?project=${encodeURIComponent(d.id)}&version=${encodeURIComponent(v.id)}`);
      v.changelog = r.changelog || "";
      $(".ver-log", det).innerHTML = v.changelog ? renderRich(v.changelog, "html") : `<span class="muted">${esc(tr("Kein Änderungsprotokoll."))}</span>`;
    } catch (e) { $(".ver-log", det).innerHTML = `<span class="muted">${esc(e.message)}</span>`; }
  }));
  const first = $("details.ver[open]", md);
  if (first) first.dispatchEvent(new Event("toggle"));
  if ($("#detSel", md)) $("#detSel", md).onclick = () => { opts.toggle(); $("#detSel", md).textContent = opts.selected() ? "✓ " + tr("Ausgewählt") : "+ " + tr("Auswählen"); };
}

// ---------- crash help ----------
async function renderCrash(body, inst) {
  body.innerHTML = loading(tr("Suche den letzten Absturz …"));
  let r;
  try { r = await api("/api/crash?id=" + encodeURIComponent(inst.id)); }
  catch (e) { body.innerHTML = `<div class="banner banner-err"><span class="b-ico">✕</span><div>${esc(e.message)}</div></div>`; return; }
  if (!r.found) {
    body.innerHTML = `<div class="card empty"><div class="big">🎉</div><b>${esc(tr("Kein Absturz gefunden."))}</b><div style="margin-top:6px">${esc(tr("Stürzt Minecraft ab, schau hier nach – CraftKit liest dann den Absturzbericht und sagt dir, woran es liegt."))}</div>
      <div style="margin-top:14px"><button class="btn" id="crRe">${esc(tr("Erneut prüfen"))}</button></div></div>`;
    $("#crRe").onclick = () => renderCrash(body, inst);
    return;
  }
  const icons = { missing: "🧩", incompatible: "⚔", suspect: "🔎", memory: "🧠", java: "☕", mixin: "🧬", info: "ℹ" };
  const modBtns = (m) => {
    const [src, pid] = (m.key || "").split(":");
    return `<div class="crash-mod"><b>${esc(m.name)}</b>${m.disabled ? ` <span class="pill">${esc(tr("deaktiviert"))}</span>` : ""}${m.hits ? ` <span class="muted">(${esc(tr("{0}× im Fehlerverlauf", m.hits))})</span>` : ""}
      <span class="spacer"></span>
      ${m.key ? `<button class="btn btn-sm" data-cupd="${esc(src)}:${esc(pid)}">↻ ${esc(tr("Aktualisieren"))}</button><button class="btn btn-sm" data-cold="${esc(m.key)}">⬇ ${esc(tr("Ältere Version"))}</button><button class="btn btn-sm btn-ghost" data-cdet="${esc(src)}:${esc(pid)}" data-cname="${esc(m.name)}">${esc(tr("Details"))}</button>` : ""}
      ${(m.key || m.file) && !m.disabled ? `<button class="btn btn-sm btn-danger" data-coff="${esc(m.key || "")}" data-cfile="${esc(m.file || "")}" data-cname="${esc(m.name)}">${esc(tr("Deaktivieren"))}</button>` : ""}</div>`;
  };
  body.innerHTML = `
    <div class="card card-pad" style="margin-bottom:14px;display:flex;gap:14px;align-items:flex-start">
      <div style="font-size:26px">💥</div>
      <div style="flex:1;min-width:0"><b>${esc(tr("Letzter Absturz: {0}", timeAgo(r.time)))}</b> <span class="muted">(${esc(fmtDateTime(r.time))})</span>
        ${r.description ? `<div class="muted" style="margin-top:2px">${esc(r.description)}</div>` : ""}
        <div class="mono muted" style="font-size:11.5px;margin-top:4px;word-break:break-all">${esc(r.file)}</div></div>
      <button class="btn btn-sm" id="crOpen">${esc(tr("Bericht öffnen"))}</button><button class="btn btn-sm" id="crRe">${esc(tr("Erneut prüfen"))}</button></div>
    ${r.findings.map((f, i) => `<div class="card crash-finding kind-${esc(f.kind)}">
      <div class="cf-head"><span class="cf-ico">${icons[f.kind] || "•"}</span><div style="flex:1"><b>${esc(f.title)}</b>${f.detail ? `<div class="muted" style="margin-top:3px">${esc(f.detail)}</div>` : ""}</div></div>
      ${(f.missing || []).length ? `<div class="cf-body">${f.missing.map(m => `<div class="crash-mod"><b>${esc(m.name || m.modId)}</b> <span class="muted">– ${esc(tr("benötigt von {0}", (m.neededBy || []).join(", ")))}${m.projectId ? "" : " · " + esc(tr("nicht automatisch gefunden"))}</span></div>`).join("")}
        ${f.missing.some(m => m.projectId) ? `<button class="btn btn-sm btn-primary" data-cmiss="${i}" style="margin-top:8px">${esc(tr("Fehlende installieren"))}</button>` : ""}</div>` : ""}
      ${(f.mods || []).length ? `<div class="cf-body">${f.mods.map(modBtns).join("")}</div>` : ""}
      ${f.kind === "memory" ? `<div class="cf-body"><button class="btn btn-sm btn-primary" id="crRam">${esc(tr("Arbeitsspeicher einstellen"))}</button></div>` : ""}
    </div>`).join("")}
    ${r.excerpt ? `<details style="margin-top:12px"><summary class="muted" style="cursor:pointer">${esc(tr("Auszug aus dem Bericht anzeigen"))}</summary><pre class="log" style="height:auto;max-height:320px">${esc(r.excerpt)}</pre></details>` : ""}`;
  $("#crOpen").onclick = () => api("/api/open-folder", { path: r.file });
  $("#crRe").onclick = () => renderCrash(body, inst);
  if ($("#crRam")) $("#crRam").onclick = () => instanceSettingsModal(inst);
  $$("[data-cmiss]", body).forEach(b => b.onclick = () => {
    const f = r.findings[+b.dataset.cmiss];
    makePlan({ type: "instance", id: inst.id }, f.missing.filter(m => m.projectId).map(m => ({ source: m.source, projectId: m.projectId })));
  });
  $$("[data-cupd]", body).forEach(b => b.onclick = () => {
    const [source, projectId] = b.dataset.cupd.split(":");
    makePlan({ type: "instance", id: inst.id }, [{ source, projectId }]);
  });
  $$("[data-cold]", body).forEach(b => b.onclick = async () => {
    try {
      const d = await api(`/api/target?type=instance&id=${encodeURIComponent(inst.id)}`);
      const it = (d.items || []).find(x => x.key === b.dataset.cold);
      if (it) changeVersion(d.target, it);
    } catch (e) { toast(e.message, true); }
  });
  $$("[data-cdet]", body).forEach(b => b.onclick = () => { const [source, pid] = b.dataset.cdet.split(":"); detailsModal(source, pid, b.dataset.cname, {}); });
  $$("[data-coff]", body).forEach(b => b.onclick = async () => {
    try {
      await api("/api/toggle", { type: "instance", id: inst.id, key: b.dataset.coff, file: b.dataset.coff ? "" : b.dataset.cfile, enabled: false });
      toast(tr("„{0}“ deaktiviert – starte Minecraft erneut.", b.dataset.cname));
      renderCrash(body, inst);
    } catch (e) { toast(e.message, true); }
  });
}

// ---------- worlds ----------
async function renderWorlds(body, inst) {
  body.innerHTML = loading(tr("Lese Welten …"));
  let d;
  try { d = await api("/api/worlds?id=" + encodeURIComponent(inst.id)); }
  catch (e) { body.innerHTML = `<div class="banner banner-err"><span class="b-ico">✕</span><div>${esc(e.message)}</div></div>`; return; }
  const worlds = (d.worlds || []).map(w => ({ ...w, backups: w.backups || [] }));
  body.innerHTML = `
    <div class="banner banner-info"><span class="b-ico">🛟</span><div style="flex:1">${trh("Sicherungen liegen in {0}. Pro Welt bleiben die {1} neuesten erhalten.", `<span class="mono">${esc(d.backupDir)}</span>`, 5)}
      ${esc(d.auto ? tr("Vor Mod-Updates sichert CraftKit kürzlich gespielte Welten automatisch.") : tr("Die automatische Sicherung vor Mod-Updates ist in den Einstellungen ausgeschaltet."))}</div>
      <button class="btn btn-sm" id="wbFolder">📁 ${esc(tr("Öffnen"))}</button></div>
    ${worlds.length ? worlds.map((w, i) => `<div class="card" style="margin-bottom:10px">
      <div class="row" style="border-bottom:${w.backups.length ? "1px solid var(--line)" : "none"}">
        <div class="mod-icon">🌍</div>
        <div class="grow"><div class="row-title">${esc(w.folder)}${w.lastPlayed ? "" : `<span class="pill pill-gold">${esc(tr("nur noch als Sicherung"))}</span>`}</div>
          <div class="row-meta">${w.lastPlayed ? `${esc(tr("zuletzt gespielt {0}", timeAgo(w.lastPlayed)))} · ${fmtSize(w.sizeBytes)} · ` : ""}${esc(tr("{0} Sicherung(en)", w.backups.length))}</div></div>
        ${w.lastPlayed ? `<button class="btn btn-sm btn-primary" data-wbackup="${i}">${esc(tr("Jetzt sichern"))}</button>` : ""}
      </div>
      ${w.backups.map((b, k) => `<div class="row plan-row" style="padding-left:66px">
        <div class="grow"><div class="row-title" style="font-weight:500">${esc(fmtDateTime(b.created))}${b.auto ? `<span class="pill">${esc(tr("automatisch"))}</span>` : ""}</div>
          <div class="row-meta">${fmtSize(b.sizeBytes)}</div></div>
        <button class="btn btn-sm" data-wrestore="${i}:${k}">${esc(tr("Wiederherstellen"))}</button>
        <button class="btn btn-sm btn-ghost" data-wdel="${i}:${k}" title="${esc(tr("Sicherung löschen"))}">✕</button></div>`).join("")}
    </div>`).join("") : `<div class="card empty"><div class="big">🌍</div>${esc(tr("Noch keine Welten in dieser Instanz."))}</div>`}`;
  $("#wbFolder").onclick = () => api("/api/open-folder", { path: d.backupDir });
  $$("[data-wbackup]", body).forEach(b => b.onclick = async () => {
    const w = worlds[+b.dataset.wbackup];
    try {
      const { job } = await api("/api/worlds/backup", { id: inst.id, folder: w.folder });
      jobModal(job, tr("„{0}“ wird gesichert", w.folder), async (r, jmd) => { closeModal(jmd); toast(tr("„{0}“ gesichert.", w.folder)); renderWorlds(body, inst); });
    } catch (e) { toast(e.message, true); }
  });
  $$("[data-wrestore]", body).forEach(b => b.onclick = () => {
    const [i, k] = b.dataset.wrestore.split(":").map(Number), w = worlds[i], bk = w.backups[k];
    confirmModal(tr("Welt wiederherstellen?"), `<p>${trh("„{0}“ wird auf den Stand vom {1} zurückgesetzt.", esc(w.folder), `<b>${esc(fmtDateTime(bk.created))}</b>`)}</p>
      <p class="muted">${esc(tr("Der aktuelle Stand wird vorher automatisch gesichert – du kannst also jederzeit zurück. Minecraft muss dafür geschlossen sein."))}</p>`, tr("Wiederherstellen"), async () => {
      const { job } = await api("/api/worlds/restore", { id: inst.id, folder: w.folder, file: bk.file });
      jobModal(job, tr("„{0}“ wird wiederhergestellt", w.folder), async (r, jmd) => { closeModal(jmd); toast(tr("„{0}“ wiederhergestellt.", w.folder)); renderWorlds(body, inst); });
    });
  });
  $$("[data-wdel]", body).forEach(b => b.onclick = () => {
    const [i, k] = b.dataset.wdel.split(":").map(Number), w = worlds[i], bk = w.backups[k];
    confirmModal(tr("Sicherung löschen?"), `<p>${esc(tr("Die Sicherung von „{0}“ vom {1} wird gelöscht.", w.folder, fmtDateTime(bk.created)))}</p>`, tr("Löschen"), async () => {
      await api("/api/worlds/delete-backup", { id: inst.id, folder: w.folder, file: bk.file });
      renderWorlds(body, inst);
    });
  });
}

function fmtDateTime(iso) {
  const d = new Date(iso);
  return isNaN(d) ? "" : d.toLocaleString(locale(), { day: "2-digit", month: "2-digit", year: "numeric", hour: "2-digit", minute: "2-digit" });
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
    <b>${esc(tr("Fehlende Abhängigkeiten gefunden"))}</b> – ${esc(tr("ohne sie starten diese Mods vermutlich nicht:"))}
    <ul style="margin:6px 0 0;padding-left:18px">${miss.map(m => `<li><b>${esc(m.name || m.modId)}</b>${m.name && m.name.toLowerCase() !== m.modId.toLowerCase() ? ` <span class="mono muted">(${esc(m.modId)})</span>` : ""} – ${esc(tr("benötigt von {0}", m.neededBy.join(", ")))}${m.projectId ? "" : ` <span class="muted">· ${esc(tr("nicht automatisch gefunden"))}</span>`}</li>`).join("")}</ul>
    ${fixable.length ? `<button class="btn btn-sm btn-primary" style="margin-top:10px" data-fixmissing>${esc(tr("Fehlende installieren ({0})", fixable.length))}</button>` : ""}
  </div></div>`;
}

function toggleHTML(on, attr) {
  return `<label class="switch" title="${esc(on ? tr("Aktiv – klicken zum Deaktivieren") : tr("Deaktiviert – klicken zum Aktivieren"))}"><input type="checkbox" ${on ? "checked" : ""} ${attr}><span></span></label>`;
}

async function toggleFlow(t, what, enabled, box) {
  const doIt = async () => {
    await api("/api/toggle", { type: t.type, id: t.id, key: what.key || "", file: what.file || "", enabled });
    toast(enabled ? tr("„{0}“ aktiviert.", what.name) : tr("„{0}“ deaktiviert.", what.name));
    await refreshState();
    renderTarget($("#main"), t.type, t.id, "installed");
  };
  try {
    if (!enabled && what.key) {
      const chk = await api(`/api/remove-check?type=${t.type}&id=${encodeURIComponent(t.id)}&key=${encodeURIComponent(what.key)}`);
      if ((chk.dependents || []).length) {
        box.checked = true;
        confirmModal(tr("Trotzdem deaktivieren?"), `<div class="banner banner-warn"><span class="b-ico">⚠</span><div>${(chk.dependents.length === 1 ? trh("{0} benötigt „{1}“ und startet ohne sie vermutlich nicht.", `<b>${esc(chk.dependents.join(", "))}</b>`, esc(what.name)) : trh("{0} benötigen „{1}“ und starten ohne sie vermutlich nicht.", `<b>${esc(chk.dependents.join(", "))}</b>`, esc(what.name)))}</div></div>`, tr("Deaktivieren"), doIt);
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
  let html = `<p>${esc(tr("„{0}“ wird entfernt.", it.name))}</p>`;
  if (dep.length) html += `<div class="banner banner-warn"><span class="b-ico">⚠</span><div>${dep.length === 1 ? trh("{0} benötigt diesen Eintrag und startet ohne ihn wahrscheinlich nicht mehr.", `<b>${esc(dep.join(", "))}</b>`) : trh("{0} benötigen diesen Eintrag und starten ohne ihn wahrscheinlich nicht mehr.", `<b>${esc(dep.join(", "))}</b>`)}</div></div>`;
  if (orph.length) html += `<label class="check"><input type="checkbox" id="rmOrph" checked> ${esc(tr("Nicht mehr benötigte Abhängigkeiten ebenfalls entfernen: {0}", orph.join(", ")))}</label>`;
  confirmModal(dep.length ? tr("Trotzdem entfernen?") : tr("Entfernen?"), html, tr("Entfernen"), async () => {
    const withOrphans = !!($("#rmOrph") && $("#rmOrph").checked);
    const r = await api("/api/remove", { type: t.type, id: t.id, key: it.key, withOrphans });
    toast(tr("Entfernt: {0}", (r.removed || []).join(", ")));
    await refreshState();
    renderTarget($("#main"), t.type, t.id, "installed");
  });
}

// ---------- search ----------
function renderSearch(body, t) {
  const noun = KIND_NOUN[t.kind] || tr("Mods");
  if (!S.cartTarget || S.cartTarget.type !== t.type || S.cartTarget.id !== t.id) {
    S.cart = [];
    S.cartTarget = { type: t.type, id: t.id };
    renderCart();
  }
  body.innerHTML = `
    ${t.kind === "mod" ? `<div id="setsBox"></div>` : ""}
    <div class="searchbar">
      <div class="seg" id="srcSeg">${Object.entries(SOURCES).map(([k, n]) => `<button data-src="${k}" class="${S.source === k ? "active" : ""}">${n}</button>`).join("")}</div>
      <input class="input search-input" id="q" placeholder="${esc(tr("{0} suchen, z. B. {1} …", noun, KIND_EXAMPLE[t.kind] || ""))}" autocomplete="off">
    </div>
    <div class="muted" style="font-size:12.5px;margin:-4px 0 12px">${esc(t.kind === "resourcepack" ? tr("Beim Installieren wird die passendste Version für Minecraft {0} gewählt.", t.mcVersion) : t.kind === "mod" ? tr("Zeigt nur Einträge, die zu {0} und Minecraft {1} passen.", t.loaders.join(" / "), t.mcVersion) : tr("Zeigt nur Einträge, die zu {0} passen.", t.loaders.join(" / ")))} ${esc(tr("Voraussetzungen werden beim Installieren automatisch ergänzt."))}</div>
    <div id="results"></div>`;
  $$("[data-src]", body).forEach(b => b.onclick = () => {
    S.source = b.dataset.src;
    $$("[data-src]", body).forEach(x => x.classList.toggle("active", x === b));
    doSearch(t, $("#q").value, 0);
  });
  let timer;
  $("#q").oninput = () => { clearTimeout(timer); timer = setTimeout(() => doSearch(t, $("#q").value, 0), 350); };
  $("#q").focus();
  if (t.kind === "mod") renderSets(t);
  doSearch(t, "", 0);
}

let searchSeq = 0;
async function doSearch(t, q, offset) {
  const seq = ++searchSeq;
  const box = $("#results");
  if (!box) return;
  if (offset === 0) box.innerHTML = loading(tr("Suche …"));
  let res;
  try {
    res = await api(`/api/search?source=${S.source}&type=${t.type}&id=${encodeURIComponent(t.id)}&q=${encodeURIComponent(q)}&offset=${offset}`);
  } catch (e) {
    if (seq !== searchSeq) return;
    const isKey = /API-Key/i.test(e.message);
    box.innerHTML = `<div class="banner banner-err"><span class="b-ico">✕</span><div>${esc(e.message)}${isKey ? ` <button class="linkish" id="goSet">${esc(tr("Zu den Einstellungen"))}</button>` : ""}</div></div>`;
    if (isKey) $("#goSet").onclick = () => go({ type: "settings" });
    return;
  }
  if (seq !== searchSeq) return;
  const hits = res.hits || [];
  if (offset === 0 && !hits.length) {
    box.innerHTML = `<div class="card empty"><div class="big">🔍</div>${esc(tr("Nichts gefunden. Probier einen anderen Begriff oder die andere Quelle."))}</div>`;
    return;
  }
  let grid = $(".results", box);
  if (offset === 0 || !grid) { box.innerHTML = `<div class="results"></div><div id="more" style="text-align:center;margin-top:14px"></div>`; grid = $(".results", box); }
  hits.forEach(h => grid.insertAdjacentHTML("beforeend", resultCard(h)));
  bindResults(grid, t, hits);
  const shown = $$(".result", grid).length;
  $("#more").innerHTML = shown < res.total ? `<button class="btn" id="moreBtn">${esc(tr("Mehr laden ({0} weitere)", fmtNum(res.total - shown)))}</button>` : "";
  if ($("#moreBtn")) $("#moreBtn").onclick = () => doSearch(t, q, shown);
}

function inCart(source, id) { return S.cart.find(c => c.source === source && c.projectId === id); }

function resultCard(h) {
  const sel = inCart(h.source, h.id);
  return `<div class="result ${sel ? "selected" : ""}" data-rid="${esc(h.source + ":" + h.id)}">
    ${iconHTML(h.iconUrl, h.name)}
    <div class="grow">
      <button class="result-title title-link" data-det title="${esc(tr("Details anzeigen"))}">${esc(h.name)}</button>
      <div class="result-sum">${esc(h.summary)}</div>
      <div class="result-foot">
        <span>⬇ ${fmtNum(h.downloads)}</span>${h.author ? `<span>· ${esc(h.author)}</span>` : ""}
        <span class="spacer"></span>
        ${h.installed ? `<span class="pill pill-green">${esc(tr("installiert"))}</span>` : ""}
        <button class="linkish" data-ver>${esc(sel && sel.versionLabel ? sel.versionLabel : tr("Version"))}</button>
        ${h.pageUrl ? `<a class="linkish" href="${esc(h.pageUrl)}" data-ext>${esc(tr("Seite"))}</a>` : ""}
        <button class="btn btn-sm ${sel ? "btn-primary" : ""}" data-add>${esc(sel ? "✓ " + tr("Ausgewählt") : h.installed ? tr("Neu installieren") : "+ " + tr("Auswählen"))}</button>
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
    $("[data-det]", card).onclick = () => detailsModal(h.source, h.id, h.name, {
      selected: () => !!inCart(h.source, h.id),
      toggle: () => document.querySelector(`[data-rid="${CSS.escape(h.source + ":" + h.id)}"] [data-add]`)?.click(),
    });
    $("[data-ver]", card).onclick = () => versionPicker(t, h, v => {
      let c = inCart(h.source, h.id);
      if (!c) { c = { source: h.source, projectId: h.id, name: h.name, iconUrl: h.iconUrl }; S.cart.push(c); }
      c.versionId = v ? v.id : "";
      c.versionLabel = v ? v.number : "";
      refresh();
    });
  });
}

// changeVersion lets the user pick another (e.g. older) version of an installed item.
function changeVersion(t, it) {
  versionPicker(t, { source: it.source, id: it.projectId, name: it.name, installedVersion: it.versionId }, async v => {
    if (!v) {
      // back to the newest version: release the pin, then update normally
      if (it.pinned) await api("/api/pin", { type: t.type, id: t.id, key: it.key, pinned: false }).catch(() => {});
      makePlan({ type: t.type, id: t.id }, [{ source: it.source, projectId: it.projectId }]);
      return;
    }
    makePlan({ type: t.type, id: t.id }, [{ source: it.source, projectId: it.projectId, versionId: v.id, pin: true }]);
  });
}

async function versionPicker(t, h, onPick) {
  const md = modal(`<div class="modal-head"><div style="flex:1"><h2>${esc(tr("Version von {0}", h.name))}</h2><div class="sub">${esc(t.kind === "plugin" ? tr("Nur Versionen, die zu dieser Server-Software passen.") : tr("Nur Versionen, die zu dieser Instanz passen."))}${h.installedVersion ? " " + esc(tr("Wählst du eine ältere Version, wird sie festgehalten, damit Updates sie nicht ersetzen.")) : ""}</div></div></div>
    <div class="modal-body" id="vpBody">${loading()}</div>
    <div class="modal-foot"><button class="btn btn-ghost" data-close>${esc(tr("Abbrechen"))}</button></div>`);
  try {
    const list = await api(`/api/versions?source=${h.source}&type=${t.type}&id=${encodeURIComponent(t.id)}&project=${encodeURIComponent(h.id)}`);
    if (!list || !list.length) { $("#vpBody", md).innerHTML = `<div class="empty">${esc(tr("Keine passende Version gefunden."))}</div>`; return; }
    $("#vpBody", md).innerHTML = `<div class="list card">
      <div class="row" style="cursor:pointer" data-v=""><div class="grow"><div class="row-title">${esc(h.installedVersion ? tr("Neueste stabile Version (nicht festhalten)") : tr("Automatisch (neueste stabile)"))}</div><div class="row-meta">${esc(tr("empfohlen"))}</div></div></div>
      ${list.map((v, i) => `<div class="row" style="cursor:pointer" data-v="${i}"><div class="grow">
        <div class="row-title"><span class="mono">${esc(v.number)}</span>${v.type !== "release" ? `<span class="pill pill-gold">${esc(v.type)}</span>` : ""}${h.installedVersion === v.id ? `<span class="pill pill-green">${esc(tr("installiert"))}</span>` : ""}</div>
        <div class="row-meta">${fmtDate(v.date)} · ${esc((v.gameVersions || []).slice(0, 6).join(", "))}${(v.deps || []).filter(d => d.kind === "required").length ? " · " + esc(tr("{0} Voraussetzung(en)", v.deps.filter(d => d.kind === "required").length)) : ""}</div>
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
  $("#cartLabel").textContent = tr("ausgewählt:");
  $("#cartNames").textContent = S.cart.map(c => c.name).join(", ");
}
$("#btnCartClear").onclick = () => { S.cart = []; renderCart(); if (S.view.tab === "add") $$(".result.selected").forEach(r => { r.classList.remove("selected"); const b = $("[data-add]", r); b.classList.remove("btn-primary"); b.textContent = "+ " + tr("Auswählen"); }); };
$("#btnCartPlan").onclick = () => makePlan(S.cartTarget, S.cart.map(c => ({ source: c.source, projectId: c.projectId, versionId: c.versionId || "" })));

// ---------- plan ----------
async function makePlan(target, requests, updateAll = false) {
  const md = modal(`<div class="modal-head"><div style="flex:1"><h2>${esc(updateAll ? tr("Nach Updates suchen") : tr("Abhängigkeiten werden geprüft"))}</h2></div></div>
    <div class="modal-body">${loading(updateAll ? tr("Prüfe alle installierten Einträge …") : tr("Suche passende Versionen und Voraussetzungen …"))}</div>`);
  let plan;
  try { plan = await api("/api/plan", { type: target.type, id: target.id, requests, updateAll }); }
  catch (e) {
    md.querySelector(".modal").innerHTML = `<div class="modal-head"><h2>${esc(tr("Fehler"))}</h2></div><div class="modal-body"><div class="banner banner-err"><span class="b-ico">✕</span><div>${esc(e.message)}</div></div></div><div class="modal-foot"><button class="btn" data-close>${esc(tr("Schließen"))}</button></div>`;
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
  const noun = plan.target.kind === "plugin" ? tr("Plugins") : tr("Mods");

  const row = i => `<div class="row plan-row">${iconHTML(i.iconUrl, i.name)}
    <div class="grow"><div class="row-title">${esc(i.name)}
      ${i.action === "update" ? (i.downgrade ? `<span class="pill pill-blue">⬇ ${esc(tr("Ältere Version"))}</span>` : `<span class="pill pill-gold">${esc(tr("Update"))}</span>`) : ""}
      ${i.pin ? `<span class="pill" title="${esc(tr("Wird von Updates nicht ersetzt"))}">📌 ${esc(tr("festgehalten"))}</span>` : ""}
      ${i.version?.type && i.version.type !== "release" ? `<span class="pill pill-gold">${esc(i.version.type)}</span>` : ""}</div>
      <div class="row-meta">${i.action === "update" ? `<span class="mono">${esc(i.fromVersion)}</span> → ` : ""}<span class="mono">${esc(i.version?.number || "")}</span>
      ${!i.explicit && (i.requiredBy || []).length ? ` · ${trh("benötigt von {0}", `<b>${esc(i.requiredBy.join(", "))}</b>`)}` : ""}${i.note ? " · " + esc(i.note) : ""}</div></div>
    ${i.pageUrl ? `<a class="btn btn-sm btn-ghost" href="${esc(i.pageUrl)}" data-ext title="${esc(tr("Projektseite im Browser öffnen"))}">${esc(tr("Seite"))} ↗</a>` : ""}</div>`;

  const group = (title, list, extra = "") => list.length ? `<div class="plan-group"><div class="section-label" style="margin:0 0 6px">${title} · ${list.length}</div>${extra}<div class="list">${list.map(row).join("")}</div></div>` : "";
  const banners = [
    ...errs.map(e => `<div class="banner banner-err"><span class="b-ico">✕</span><div>${esc(e)}</div></div>`),
    ...conf.map(e => `<div class="banner banner-err"><span class="b-ico">⚔</span><div>${esc(e)}</div></div>`),
    ...warn.map(e => `<div class="banner banner-warn"><span class="b-ico">⚠</span><div>${esc(e)}</div></div>`),
  ].join("");

  const nothing = !todo.length && !manual.length;
  const md = modal(`<div class="modal-head"><div style="flex:1"><h2>${esc(updateAll ? tr("Updates für {0}", plan.target.name) : tr("Installationsplan für {0}", plan.target.name))}</h2>
      <div class="sub">${esc(nothing ? (updateAll ? tr("Alles ist auf dem neuesten Stand.") : tr("Es gibt nichts Neues zu installieren.")) : auto.length ? tr("{0} Datei(en) werden geladen, davon {1} automatisch als Voraussetzung.", todo.length, auto.length) : tr("{0} Datei(en) werden geladen.", todo.length))}</div></div></div>
    <div class="modal-body">
      ${banners}
      ${group(esc(updateAll ? tr("Aktualisierungen") : tr("Deine Auswahl")), updateAll ? todo : chosen)}
      ${updateAll ? "" : group(esc(tr("Wird automatisch mitinstalliert (Voraussetzungen)")), auto)}
      ${manual.length ? `<div class="plan-group"><div class="section-label" style="margin:0 0 6px">${esc(tr("Manueller Download nötig"))} · ${manual.length}</div>
        <div class="banner banner-warn" style="margin-bottom:8px"><span class="b-ico">⚠</span><div>${esc(tr("Der Autor erlaubt keine Downloads über andere Programme. Lade die Datei auf CurseForge herunter und leg sie in den Ordner (Button nach der Installation)."))}</div></div>
        <div class="list">${manual.map(row).join("")}</div></div>` : ""}
      ${opt.length ? `<div class="plan-group"><div class="section-label" style="margin:0 0 6px">${esc(tr("Optionale Erweiterungen"))} · ${opt.length}</div>
        <div class="list">${opt.map((o, i) => `<label class="row plan-row" style="cursor:pointer"><input type="checkbox" data-opt="${i}" style="accent-color:var(--green);width:16px;height:16px">
          ${iconHTML(o.iconUrl, o.name)}<div class="grow"><div class="row-title">${esc(o.name)}</div>
          ${o.summary ? `<div class="row-meta" style="white-space:normal">${esc(o.summary)}</div>` : ""}<div class="row-meta">${esc(tr("optional für {0}", o.for))}</div></div>
          ${o.pageUrl ? `<a class="btn btn-sm btn-ghost" href="${esc(o.pageUrl)}" data-ext title="${esc(tr("Projektseite im Browser öffnen"))}">${esc(tr("Seite"))} ↗</a>` : ""}</label>`).join("")}</div>
        <div style="margin-top:8px"><button class="btn btn-sm" id="optAdd" disabled>${esc(tr("Auswahl übernehmen & neu prüfen"))}</button></div></div>` : ""}
      ${keep.length ? `<details class="plan-group"><summary class="section-label" style="cursor:pointer;margin:0">${esc(tr("Bereits vorhanden"))} · ${keep.length}</summary><div class="list" style="margin-top:6px">${keep.map(row).join("")}</div></details>` : ""}
    </div>
    <div class="modal-foot">
      <span class="muted" style="font-size:12.5px">${esc(plan.target.dir)}</span><span class="spacer"></span>
      <button class="btn btn-ghost" data-close>${esc(tr("Abbrechen"))}</button>
      ${nothing ? `<button class="btn btn-primary" data-close>OK</button>` :
        `<button class="btn ${errs.length || conf.length ? "btn-danger" : "btn-primary"}" id="planGo">${esc(errs.length || conf.length ? tr("Trotzdem installieren") : tr("Installieren ({0})", todo.length))}</button>`}
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
      jobModal(job, tr("{0} werden installiert", noun), async () => {
        S.cart = [];
        renderCart();
        await refreshState();
        if (target.type.startsWith(S.view.type) && S.view.id === target.id) renderTarget($("#main"), target.type, target.id, "installed");
      }, plan.target.dir);
    } catch (e) { toast(e.message, true); }
  };
}

// ---------- jobs ----------
function jobModal(jobId, title, onDone, folder) {
  const md = modal(`<div class="modal-head"><div style="flex:1"><h2>${esc(title)}</h2></div></div>
    <div class="modal-body">
      <div class="progress indet" id="jProg"><div style="width:0"></div></div>
      <div class="job-step" id="jStep">${esc(tr("Starte …"))}</div>
      <div id="jResult"></div>
      <div class="log" id="jLog"></div>
    </div>
    <div class="modal-foot" id="jFoot"><span class="muted" style="font-size:12.5px">${esc(tr("Du kannst das Fenster offen lassen, der Vorgang läuft weiter."))}</span></div>`, true, true);
  let doneCalled = false;
  const poll = async () => {
    let j;
    try { j = await api("/api/job?id=" + jobId); } catch (e) { $("#jStep", md).textContent = e.message; return; }
    const prog = $("#jProg", md);
    prog.classList.toggle("indet", j.progress < 0 && j.status === "running");
    $("div", prog).style.width = (Math.max(0, j.progress) * 100).toFixed(1) + "%";
    $("#jStep", md).textContent = j.status === "running" ? (j.step || tr("Läuft …")) : j.status === "done" ? tr("Fertig.") : tr("Fehlgeschlagen.");
    const log = $("#jLog", md);
    const atBottom = log.scrollTop + log.clientHeight >= log.scrollHeight - 20;
    log.textContent = (j.log || []).join("\n");
    if (atBottom) log.scrollTop = log.scrollHeight;
    if (j.status === "running") { setTimeout(poll, 500); return; }

    const r = j.result || {};
    let html = "";
    if (j.status === "error") html += `<div class="banner banner-err"><span class="b-ico">✕</span><div>${esc(j.error)}</div></div>`;
    if ((r.failed || []).length) html += `<div class="banner banner-err"><span class="b-ico">✕</span><div>${esc(tr("Fehlgeschlagen: {0}", r.failed.join(" · ")))}</div></div>`;
    if ((r.manual || []).length) html += `<div class="banner banner-warn"><span class="b-ico">⚠</span><div>${esc(tr("Diese Dateien erlauben keinen Download über andere Programme. Bitte auf der Website herunterladen und in den angegebenen Ordner legen:"))}<br>${r.manual.map(m => `<a href="${esc(m.url || m.version?.file?.manualUrl)}" data-ext>${esc(m.name)} (${esc(m.fileName || m.version?.file?.fileName)})</a>${m.folder ? ` <span class="muted mono" style="font-size:11.5px">→ ${esc(m.folder)}</span>` : ""}`).join("<br>")}</div></div>`;
    if (r.files && !r.installed) html += `<div class="banner banner-info"><span class="b-ico">✓</span><div>${esc(tr("{0} Dateien installiert", r.files - (r.failed || []).length - (r.manual || []).length))}${r.skipped ? ", " + esc(tr("{0} reine Server-Dateien übersprungen", r.skipped)) : ""}${r.identify ? " · " + esc(tr("{0} Mods für Updates erkannt", (r.identify.recognized || []).length)) : ""}.</div></div>`;
    if (j.status === "done" && (r.installed || r.updated)) {
      const n = (r.installed || []).length, u = (r.updated || []).length;
      html += `<div class="banner banner-info"><span class="b-ico">✓</span><div>${esc([n ? tr("{0} installiert", n) : "", u ? tr("{0} aktualisiert", u) : "", !n && !u ? tr("Nichts geändert") : ""].filter(Boolean).join(", "))}.</div></div>`;
    }
    $("#jResult", md).innerHTML = html;
    if (!folder && r.instance?.dir) folder = r.instance.dir;
    $("#jFoot", md).innerHTML = `${folder || r.dir ? `<button class="btn" id="jFolder">📁 ${esc(tr("Ordner öffnen"))}</button>` : ""}<span class="spacer"></span>
      ${j.status === "done" ? `<button class="btn btn-play btn-sm" id="jLaunch" style="height:36px">▶ ${esc(tr("Minecraft Launcher öffnen"))}</button>` : ""}
      <button class="btn btn-primary" data-close>${esc(tr("Schließen"))}</button>`;
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
    <div class="modal-foot"><button class="btn btn-ghost" data-close>${esc(tr("Abbrechen"))}</button><button class="btn btn-danger" id="cOk">${esc(okLabel)}</button></div>`);
  $("#cOk", md).onclick = async () => {
    $("#cOk", md).disabled = true;
    try { await onOk(); closeModal(md); } catch (e) { toast(e.message, true); $("#cOk", md).disabled = false; }
  };
}

function instanceSettingsModal(inst) {
  const md = modal(`<div class="modal-head"><h2>${esc(tr("Instanz bearbeiten"))}</h2></div>
    <div class="modal-body"><div class="field"><label>${esc(tr("Name"))}</label><input class="input" id="sName" value="${esc(inst.name)}" maxlength="60"></div>
    <div class="field" style="margin-top:14px"><label>${esc(tr("Arbeitsspeicher"))}</label><div class="range-row"><input type="range" id="sMem" min="0" max="16" value="${inst.memoryGB || 0}"><span class="range-val" id="sMemVal"></span></div></div>
    <div class="kv" style="margin-top:18px">
      <div>${esc(tr("Launcher-Version"))}</div><div class="mono">${esc(inst.versionId)}</div>
      <div>${esc(tr("Ordner"))}</div><div class="mono">${esc((S.state.config.instancesDir || "") + "\\" + inst.id)}</div></div>
    <div style="margin-top:14px"><button class="btn btn-sm" id="sRe">${esc(tr("Profil im Launcher neu eintragen"))}</button>
      <span class="muted" style="font-size:12px;margin-left:8px">${esc(tr("falls es im Launcher fehlt"))}</span></div></div>
    <div class="modal-foot"><button class="btn btn-ghost" data-close>${esc(tr("Abbrechen"))}</button><button class="btn btn-primary" id="sSave">${esc(tr("Speichern"))}</button></div>`);
  const lbl = () => $("#sMemVal", md).textContent = +$("#sMem", md).value === 0 ? tr("Standard") : $("#sMem", md).value + " GB";
  $("#sMem", md).oninput = lbl; lbl();
  $("#sRe", md).onclick = () => api("/api/instances/reprofile", { id: inst.id }).then(() => toast(tr("Profil eingetragen. Launcher ggf. neu starten."))).catch(e => toast(e.message, true));
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
  if (inst.adopted) {
    confirmModal(tr("Aus CraftKit entfernen?"), `<p>${esc(tr("„{0}“ wird nicht mehr von CraftKit verwaltet. Das Profil im Minecraft Launcher, deine Mods und Welten bleiben unverändert.", inst.name))}</p>`, tr("Entfernen"), async () => {
      await api("/api/instances/delete", { id: inst.id, deleteFiles: false });
      await refreshState();
      go({ type: "welcome" });
    });
    return;
  }
  confirmModal(tr("Instanz löschen?"), `<p>${esc(tr("Das Profil „{0}“ wird aus dem Minecraft Launcher entfernt.", inst.name + " (CraftKit)"))}</p>
    <label class="check"><input type="checkbox" id="dFiles"> ${esc(tr("Auch den Ordner mit Mods, Welten und Einstellungen löschen"))}</label>
    <p class="muted" style="font-size:12.5px">${esc(tr("Ohne Haken bleiben deine Welten erhalten."))}</p>`, tr("Löschen"), async () => {
    await api("/api/instances/delete", { id: inst.id, deleteFiles: $("#dFiles").checked });
    await refreshState();
    go({ type: "welcome" });
  });
}

function deletePluginFolder(pf) {
  confirmModal(tr("Plugin-Ordner entfernen?"), `<p>${esc(tr("„{0}“ wird aus CraftKit entfernt. Die Plugins im Ordner bleiben erhalten.", pf.name))}</p>`, tr("Entfernen"), async () => {
    await api("/api/plugin-folders/delete", { id: pf.id });
    await refreshState();
    go({ type: "welcome" });
  });
}

// ---------- profiles found in the launcher ----------
async function renderFound(m, key) {
  m.innerHTML = `<div class="main-inner">${loading(tr("Lese Profil und Mods …"))}</div>`;
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
        <span class="pill mono" title="${esc(tr("Version im Launcher"))}">${esc(p.versionId)}</span>
        ${p.installed ? "" : `<span class="pill pill-gold" title="${esc(tr("Die Version fehlt im versions-Ordner"))}">${esc(tr("Version nicht installiert"))}</span>`}
      </div></div>
      <div class="actions"><button class="btn btn-sm" id="fFolder">📁 ${esc(tr("Ordner"))}</button><button class="btn btn-primary" id="fAdopt">${esc(tr("In CraftKit übernehmen"))}</button></div>
    </div>
    <div class="banner banner-info"><span class="b-ico">ℹ</span><div>${esc(tr("Dieses Profil kommt aus dem offiziellen Launcher. Übernimmst du es, erkennt CraftKit die vorhandenen Mods online und kann sie dann aktualisieren, Abhängigkeiten prüfen und neue Mods dazuinstallieren. Profil, Welten und Dateien bleiben wie sie sind."))}
      <div class="mono muted" style="margin-top:4px;font-size:12px">${esc(p.gameDir)}</div></div></div>
    ${p.loader === "optifine" ? `<div class="banner banner-warn"><span class="b-ico">⚠</span><div>${esc(tr("OptiFine-Profile können keine Mods über einen Mod-Loader laden. Beim Übernehmen wird der Loader anhand der vorhandenen Mods erkannt."))}</div></div>` : ""}
    ${miss.length ? missingBanner(miss.map(x => ({ ...x, projectId: "" }))).replace("</ul>", "</ul><div class='muted' style='margin-top:6px;font-size:12.5px'>" + esc(tr("Nach dem Übernehmen kannst du fehlende Abhängigkeiten mit einem Klick installieren.")) + "</div>") : ""}
    <div class="section-label">${esc(tr("Mods im Ordner"))} · ${jars.length}</div>
    ${jars.length ? `<div class="card list">${jars.map(f => `<div class="row">${iconHTML("", f.name)}<div class="grow">
      <div class="row-title">${esc(f.name)}${f.disabled ? `<span class="pill">${esc(tr("deaktiviert"))}</span>` : ""}<span class="pill">${esc(loaderLabel(f.loader === "plugin" ? "unknown" : f.loader))}</span></div>
      <div class="row-meta">${f.version ? `<span class="mono">${esc(f.version)}</span> · ` : ""}${esc(f.file)}</div></div></div>`).join("")}</div>`
      : `<div class="card empty">${esc(tr("Keine Mods in diesem Profil."))}</div>`}
  </div>`;
  $("#fFolder").onclick = () => api("/api/open-folder", { path: p.gameDir });
  $("#fAdopt").onclick = async () => {
    try {
      const { job } = await api("/api/adopt", { key });
      jobModal(job, tr("„{0}“ wird übernommen", p.name), async (inst) => {
        await refreshState();
        if (inst && inst.id) { go({ type: "instance", id: inst.id }); toast(tr("Übernommen – CraftKit verwaltet dieses Profil jetzt.")); }
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
      <div class="row-title">${esc(info.host)}${info.port !== 25565 ? ":" + info.port : ""} <span class="pill pill-green"><span class="dot"></span>${esc(tr("online"))} · ${info.pingMs} ms</span></div>
      ${info.motd ? `<div class="muted" style="white-space:pre-wrap;margin:4px 0">${esc(info.motd)}</div>` : ""}
      <div class="head-meta" style="margin-top:6px">
        <span class="pill">${esc(tr("Version: {0}", info.versionName || "?"))}</span>
        <span class="pill">${esc(tr("{0} Spieler", info.playersOnline + "/" + info.playersMax))}</span>
        ${info.software ? `<span class="pill">${esc(info.software)}</span>` : ""}
        ${info.loader ? `<span class="pill pill-blue">${esc(tr("{0}-Server", LOADERS[info.loader]?.name || info.loader))}</span>` : ""}
        ${info.mods?.length ? `<span class="pill pill-blue">${esc(tr("{0} Mods nötig", info.mods.length))}</span>` : ""}
      </div>
      ${info.multiVersion ? `<div class="muted" style="font-size:12.5px;margin-top:6px">${esc(tr("Der Server akzeptiert mehrere Minecraft-Versionen – die vorgeschlagene ist seine eigentliche Version."))}</div>` : ""}
    </div></div>`;
}

async function serverModal(inst) {
  const md = modal(`<div class="modal-head"><div style="flex:1"><h2>🌐 ${esc(tr("Mit Server abgleichen"))}</h2>
      <div class="sub">${esc(inst ? tr("Prüft, ob „{0}“ zum Server passt, und passt die Installation bei Bedarf an.", inst.name) : tr("Richtet eine Installation ein, die genau zum Server passt."))}</div></div></div>
    <div class="modal-body">
      <div class="field"><label>${esc(tr("Server-Adresse"))}</label>
        <div style="display:flex;gap:8px"><input class="input" id="sAddr" style="flex:1" placeholder="${esc(tr("z. B. {0} oder {1}", "play.meinserver.at", "192.168.0.10:25565"))}" value="${esc(inst?.serverAddress || "")}">
        <button class="btn btn-primary" id="sPing">${esc(tr("Prüfen"))}</button></div>
        <button class="linkish" id="sManual" style="align-self:flex-start;margin-top:4px">${esc(tr("oder Version von Hand angeben"))}</button></div>
      <div id="sInfo"></div>
      <div id="sCfg" class="hidden" style="margin-top:16px">
        <div class="form-grid">
          <div class="field"><label>${esc(tr("Minecraft-Version des Servers"))}</label><select class="input" id="sMc"></select></div>
          <div class="field"><label>${esc(tr("Loader"))}</label><select class="input" id="sLoader">${Object.entries(LOADERS).map(([k, l]) => `<option value="${k}">${l.name}</option>`).join("")}</select>
            <span class="hint" id="sLoaderHint"></span></div>
        </div>
        <div id="sMods"></div>
        <div id="sVerdict" style="margin-top:14px"></div>
      </div>
    </div>
    <div class="modal-foot"><button class="btn btn-ghost" data-close>${esc(tr("Abbrechen"))}</button><button class="btn btn-primary hidden" id="sGo"></button></div>`, true);

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
    $("#sMods", md).innerHTML = mods.length ? `<div class="section-label">${esc(tr("Mods, die der Server verlangt"))} · ${mods.length}${info.modsTruncated ? " " + esc(tr("(Liste gekürzt)")) : ""}</div>
      <div class="card list" style="max-height:200px;overflow:auto">${mods.map((mo, i) => `<label class="row plan-row" style="cursor:pointer"><input type="checkbox" data-smod="${i}" checked style="accent-color:var(--green);width:16px;height:16px">
        <div class="grow"><div class="row-title mono">${esc(mo.modId)}</div><div class="row-meta">${esc(mo.version || "")}</div></div></label>`).join("")}</div>
      <div class="muted" style="font-size:12.5px;margin-top:6px">${esc(tr("Werden auf Modrinth gesucht und samt Abhängigkeiten installiert."))}</div>` : "";
    verdict();
  };
  const selectedMods = () => $$("[data-smod]", md).filter(b => b.checked).map(b => info.mods[+b.dataset.smod].modId);
  const verdict = () => {
    const mc = $("#sMc", md).value, loader = $("#sLoader", md).value;
    const serverLoader = info?.loader || "";
    $("#sLoaderHint", md).textContent = serverLoader
      ? (loader === serverLoader ? tr("Der Server nutzt {0}.", LOADERS[serverLoader]?.name || serverLoader) : tr("Achtung: Der Server nutzt {0}.", LOADERS[serverLoader]?.name || serverLoader))
      : tr("Der Server verlangt keine Mods – Client-Mods wie Sodium kannst du trotzdem nutzen.");
    const go = $("#sGo", md);
    go.classList.remove("hidden");
    const nMods = inst ? Object.values(inst.mods || {}).filter(x => x.explicit).length : 0;
    const loaderOk = !serverLoader || loader === serverLoader;
    if (inst && mc === inst.mcVersion && loader === inst.loader) {
      $("#sVerdict", md).innerHTML = `<div class="banner banner-info" style="margin:0"><span class="b-ico">✓</span><div><b>${esc(tr("Passt!"))}</b> ${esc(loaderOk ? tr("„{0}“ läuft bereits auf Minecraft {1}.", inst.name, mc) : tr("„{0}“ läuft bereits auf Minecraft {1} – aber mit einem anderen Loader als der Server.", inst.name, mc))}</div></div>`;
      go.textContent = selectedMods().length ? tr("Server merken & {0} Server-Mods installieren", selectedMods().length) : tr("Server merken & in Mehrspieler-Liste eintragen");
      go.onclick = linkOnly;
    } else {
      const diff = inst ? `<div class="banner banner-warn" style="margin:0"><span class="b-ico">⚠</span><div>${trh("Der Server läuft auf {0}, deine Instanz auf {1}.", `<b>${esc(loaderLabel(loader))} ${esc(mc)}</b>`, `<b>${esc(loaderLabel(inst.loader))} ${esc(inst.mcVersion)}</b>`)}
          ${esc(nMods ? tr("CraftKit legt eine passende Instanz an, übernimmt Einstellungen, Ressourcenpakete und deine {0} Mods in der jeweils passenden Version (samt Abhängigkeiten) und trägt den Server ein. Deine bisherige Instanz bleibt unverändert.", nMods) : tr("CraftKit legt eine passende Instanz an, übernimmt Einstellungen und Ressourcenpakete und trägt den Server ein. Deine bisherige Instanz bleibt unverändert."))}
          ${loader !== inst.loader && nMods ? `<br><span class="muted">${esc(tr("Der Loader wechselt – es werden nur Mods übernommen, die es auch für {0} gibt.", loaderLabel(loader)))}</span>` : ""}</div></div>`
        : `<div class="banner banner-info" style="margin:0"><span class="b-ico">ℹ</span><div>${trh("CraftKit legt eine Instanz mit {0} an und trägt den Server in die Mehrspieler-Liste ein.", `<b>${esc(loaderLabel(loader))} ${esc(mc)}</b>`)}</div></div>`;
      $("#sVerdict", md).innerHTML = diff;
      go.textContent = inst ? tr("Passende Instanz anlegen") : tr("Instanz anlegen");
      go.onclick = adapt;
    }
  };
  const address = () => $("#sAddr", md).value.trim();
  const linkOnly = async () => {
    try {
      const r = await api("/api/server/link", { id: inst.id, address: address(), name: info?.host || address(), serverMods: selectedMods() });
      closeModal(md);
      toast(r.addedToList ? tr("Server in die Mehrspieler-Liste eingetragen.") : tr("Server gespeichert."));
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
      jobModal(job, tr("Instanz für {0} wird angelegt", loaderLabel(loader) + " " + mc), async (res, jmd) => {
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
    if (!address()) return toast(tr("Bitte eine Server-Adresse eingeben."), true);
    $("#sInfo", md).innerHTML = loading(tr("Frage den Server ab …"));
    $("#sPing", md).disabled = true;
    try {
      info = await api("/api/server/ping", { address: address() });
      $("#sInfo", md).innerHTML = serverCard(info);
      showCfg();
    } catch (e) {
      info = null;
      $("#sInfo", md).innerHTML = `<div class="banner banner-err" style="margin-top:14px"><span class="b-ico">✕</span><div>${esc(e.message)}<br><span class="muted">${esc(tr("Ist der Server offline? Du kannst die Version auch von Hand angeben."))}</span></div></div>`;
      showCfg();
    } finally { $("#sPing", md).disabled = false; }
  };
  $("#sPing", md).onclick = doPing;
  $("#sAddr", md).onkeydown = e => { if (e.key === "Enter") doPing(); };
  if (inst?.serverAddress) doPing(); else $("#sAddr", md).focus();
}

// ---------- share (export as .mrpack) ----------
function shareModal(inst) {
  const today = new Date().toISOString().slice(0, 10).replace(/-/g, ".");
  const md = modal(`<div class="modal-head"><div style="flex:1"><h2>📤 ${esc(tr("Instanz teilen"))}</h2>
      <div class="sub">${esc(tr("Erstellt eine Modpack-Datei (.mrpack). Deine Freunde ziehen sie in CraftKit (Neue Instanz → 📦 Modpack) oder öffnen sie mit dem Modrinth-Launcher – und haben genau deine Mods in denselben Versionen."))}</div></div></div>
    <div class="modal-body">
      <div class="form-grid">
        <div class="field"><label>${esc(tr("Name des Modpacks"))}</label><input class="input" id="eName" value="${esc(inst.name)}" maxlength="60"></div>
        <div class="field"><label>${esc(tr("Version"))}</label><input class="input" id="eVer" value="${esc(today)}" maxlength="30"></div>
      </div>
      <div class="section-label">${esc(tr("Was soll mit?"))}</div>
      <div style="display:grid;gap:8px">
        <label class="check"><input type="checkbox" id="eCfg" checked> ${esc(tr("Mod-Einstellungen (Ordner config)"))}</label>
        <label class="check"><input type="checkbox" id="eRp" checked> ${esc(tr("Ressourcenpakete"))}</label>
        <label class="check"><input type="checkbox" id="eSh" checked> ${esc(tr("Shader"))}</label>
        <label class="check"><input type="checkbox" id="eSrv" checked> ${esc(tr("Server-Liste (Mehrspieler)"))}</label>
        <label class="check"><input type="checkbox" id="eOpt"> ${esc(tr("Eigene Spieleinstellungen (Grafik, Tastenbelegung)"))}</label>
      </div>
      <p class="muted" style="font-size:12.5px;margin-top:12px">${esc(tr("Mods von Modrinth werden nur verlinkt, dadurch bleibt die Datei klein. Andere Mods (CurseForge, selbst hinzugefügte) werden mitgepackt. Welten werden nicht geteilt."))}</p>
      <div id="eResult"></div>
    </div>
    <div class="modal-foot"><button class="btn btn-ghost" data-close>${esc(tr("Schließen"))}</button><button class="btn btn-primary" id="eGo">${esc(tr("Exportieren"))}</button></div>`);
  $("#eGo", md).onclick = async () => {
    const btn = $("#eGo", md);
    btn.disabled = true;
    btn.innerHTML = `<span class="spinner" style="width:14px;height:14px"></span> ${esc(tr("Exportiere …"))}`;
    try {
      const r = await api("/api/export", { id: inst.id, name: $("#eName", md).value, version: $("#eVer", md).value,
        config: $("#eCfg", md).checked, resourcePacks: $("#eRp", md).checked, shaderPacks: $("#eSh", md).checked,
        servers: $("#eSrv", md).checked, options: $("#eOpt", md).checked });
      const a = document.createElement("a");
      a.href = `/api/export/download?key=${r.key}&t=${TOKEN}`;
      a.download = r.fileName;
      document.body.appendChild(a); a.click(); a.remove();
      $("#eResult", md).innerHTML = `<div class="banner banner-info" style="margin:12px 0 0"><span class="b-ico">✓</span><div>
        ${trh("{0} wurde in deinen Downloads gespeichert.", `<b>${esc(r.fileName)}</b> (${fmtSize(r.size)})`)}<br>
        ${esc(tr("{0} Mod(s) verlinkt", r.linked))}${(r.packed || []).length ? ", " + esc(tr("{0} mitgepackt: {1}", r.packed.length, r.packed.join(", "))) : ""}.</div></div>`;
      btn.textContent = tr("Erneut exportieren");
    } catch (e) { toast(e.message, true); btn.textContent = tr("Exportieren"); }
    btn.disabled = false;
  };
}

// ---------- move an instance to another Minecraft version ----------
function cmpMC(a, b) {
  const pa = a.split(/[.-]/).map(n => parseInt(n) || 0), pb = b.split(/[.-]/).map(n => parseInt(n) || 0);
  for (let i = 0; i < Math.max(pa.length, pb.length); i++) { if ((pa[i] || 0) !== (pb[i] || 0)) return (pa[i] || 0) - (pb[i] || 0); }
  return 0;
}

async function upgradeModal(inst) {
  const mods = Object.values(inst.mods || {}).filter(m => m.explicit);
  const md = modal(`<div class="modal-head"><div style="flex:1"><h2>⬆ ${esc(tr("Auf andere Minecraft-Version wechseln"))}</h2>
      <div class="sub">${esc(mods.length ? tr("CraftKit legt eine neue Instanz an und übernimmt deine {0} Mods in der jeweils passenden Version (samt Abhängigkeiten), Einstellungen und Ressourcenpakete. „{1}“ bleibt unverändert.", mods.length, inst.name) : tr("CraftKit legt eine neue Instanz an und übernimmt Einstellungen und Ressourcenpakete. „{0}“ bleibt unverändert.", inst.name))}</div></div></div>
    <div class="modal-body">
      <div class="form-grid">
        <div class="field"><label>${esc(tr("Neue Minecraft-Version"))}</label><select class="input" id="uMc"><option>${esc(tr("Lade …"))}</option></select></div>
        <div class="field"><label>${esc(tr("Loader"))}</label><select class="input" id="uLoader">${Object.entries(LOADERS).map(([k, l]) => `<option value="${k}" ${k === inst.loader ? "selected" : ""}>${l.name}</option>`).join("")}</select>
          <span class="hint">${esc(tr("Beim Wechsel des Loaders werden nur Mods übernommen, die es auch dafür gibt."))}</span></div>
      </div>
      <div id="uPreview" style="margin-top:14px"></div>
      <label class="check" style="margin-top:14px"><input type="checkbox" id="uSaves"> ${esc(tr("Welten mitnehmen (als Kopie)"))}</label>
      <div id="uWarn" style="margin-top:12px"></div>
    </div>
    <div class="modal-foot"><button class="btn btn-ghost" data-close>${esc(tr("Abbrechen"))}</button><button class="btn btn-primary" id="uGo" disabled>${esc(tr("Neue Instanz anlegen"))}</button></div>`, true);
  const loadVersions = async () => {
    const loader = $("#uLoader", md).value;
    $("#uMc", md).innerHTML = `<option>${esc(tr("Lade …"))}</option>`;
    $("#uGo", md).disabled = true;
    try {
      const list = await api(`/api/game-versions?loader=${loader}&snapshots=${S.state.config.showSnapshots ? 1 : 0}`);
      const newer = list.filter(v => cmpMC(v.id, inst.mcVersion) > 0);
      const def = (newer[0] || list[0]).id;
      $("#uMc", md).innerHTML = list.map(v => `<option value="${esc(v.id)}" ${v.id === def ? "selected" : ""}>${esc(v.id)}${v.id === inst.mcVersion ? "  " + esc(tr("(aktuell)")) : ""}</option>`).join("");
      $("#uGo", md).disabled = false;
      warn();
    } catch (e) { $("#uWarn", md).innerHTML = `<div class="banner banner-err" style="margin:0"><span class="b-ico">✕</span><div>${esc(e.message)}</div></div>`; }
  };
  const warn = () => {
    const mc = $("#uMc", md).value, same = mc === inst.mcVersion && $("#uLoader", md).value === inst.loader;
    const older = cmpMC(mc, inst.mcVersion) < 0;
    let h = "";
    if (same) h += `<div class="banner banner-info" style="margin:0 0 8px"><span class="b-ico">ℹ</span><div>${esc(tr("Das ist die aktuelle Version – es entsteht eine Kopie der Instanz."))}</div></div>`;
    if (older) h += `<div class="banner banner-warn" style="margin:0 0 8px"><span class="b-ico">⚠</span><div>${esc(tr("Das ist eine ältere Version. Welten aus neueren Versionen können darin beschädigt werden – nimm sie besser nicht mit."))}</div></div>`;
    if ($("#uSaves", md).checked) h += `<div class="banner banner-info" style="margin:0"><span class="b-ico">ℹ</span><div>${esc(tr("Die Welten werden kopiert. Sobald du eine Kopie in der neuen Version öffnest, wird sie umgewandelt – die Originale in „{0}“ bleiben unberührt.", inst.name))}</div></div>`;
    $("#uWarn", md).innerHTML = h;
    preview();
  };
  let pvSeq = 0, pvTimer;
  const preview = () => {
    clearTimeout(pvTimer);
    const box = $("#uPreview", md);
    if (!mods.length) { box.innerHTML = ""; return; }
    const mc = $("#uMc", md).value, loader = $("#uLoader", md).value;
    if (loader === "vanilla") { box.innerHTML = `<div class="banner banner-warn" style="margin:0"><span class="b-ico">⚠</span><div>${esc(tr("Vanilla lädt keine Mods – deine {0} Mods werden nicht übernommen.", mods.length))}</div></div>`; return; }
    box.innerHTML = loading(tr("Prüfe, welche deiner {0} Mods es für {1} gibt …", mods.length, loaderLabel(loader) + " " + mc));
    const seq = ++pvSeq;
    pvTimer = setTimeout(async () => {
      let list;
      try { list = await api(`/api/preview-version?id=${encodeURIComponent(inst.id)}&mc=${encodeURIComponent(mc)}&loader=${loader}`); }
      catch (e) { if (seq === pvSeq) box.innerHTML = ""; return; }
      if (seq !== pvSeq) return;
      const ok = list.filter(x => x.available), missing = list.filter(x => !x.available);
      box.innerHTML = `<div class="card" style="padding:12px 14px">
        <div style="display:flex;align-items:center;gap:10px"><b>${esc(tr("Vorschau:"))}</b>
          <span class="pill ${missing.length ? "pill-gold" : "pill-green"}">${esc(tr("{0} von {1} Mods verfügbar", ok.length, list.length))}</span></div>
        <div class="progress" style="margin:10px 0 8px"><div style="width:${list.length ? ok.length / list.length * 100 : 0}%"></div></div>
        ${missing.length ? `<div style="font-size:13px"><span class="muted">${esc(tr("Gibt es (noch) nicht für {0}:", loaderLabel(loader) + " " + mc))}</span> ${missing.map(x => `<b>${esc(x.name)}</b>`).join(", ")}</div>` : `<div style="font-size:13px" class="muted">${esc(tr("Alle deine Mods gibt es für diese Version."))}</div>`}
        ${ok.length ? `<details style="margin-top:6px;font-size:13px"><summary class="muted" style="cursor:pointer">${esc(tr("Verfügbare anzeigen"))}</summary><div style="margin-top:6px">${ok.map(x => `${esc(x.name)} <span class="mono muted">${esc(x.version)}</span>`).join(" · ")}</div></details>` : ""}
      </div>`;
    }, 400);
  };
  $("#uLoader", md).onchange = loadVersions;
  $("#uMc", md).onchange = warn;
  $("#uSaves", md).onchange = warn;
  $("#uGo", md).onclick = async () => {
    const mc = $("#uMc", md).value, loader = $("#uLoader", md).value;
    try {
      const { job } = await api("/api/server/adapt", { sourceId: inst.id, mcVersion: mc, loader, copySaves: $("#uSaves", md).checked });
      closeModal(md);
      jobModal(job, tr("Instanz für {0} wird angelegt", loaderLabel(loader) + " " + mc), async (res, jmd) => {
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
    pill.innerHTML = ok ? `<span class="dot"></span>${esc(inst.serverAddress)} · ${esc(tr("passt"))}` : `<span class="dot"></span>${esc(inst.serverAddress)} · ${esc(tr("Server hat {0} – anpassen", loaderLabel(info.loader || inst.loader) + " " + info.mcVersion))}`;
  } catch {
    if (!document.body.contains(pill)) return;
    pill.className = "pill";
    pill.innerHTML = `🌐 ${esc(inst.serverAddress)} · ${esc(tr("nicht erreichbar"))}`;
  }
}

// ---------- settings ----------
const REPO_URL = "https://github.com/mariofritzer/Craftkit";

// reportTranslation opens a prefilled GitHub issue for translation mistakes.
function reportTranslation() {
  const p = new URLSearchParams({ template: "translation.yml", title: `[${LANG}] `, language: `${LANGS[LANG]} (${LANG})`, version: S.state.version || "" });
  openExternal(`${REPO_URL}/issues/new?${p}`);
}

function renderSettings(m) {
  const c = S.state.config;
  const langOpts = `<option value="" ${!c.language ? "selected" : ""}>${esc(tr("Automatisch (Systemsprache)"))}</option>` +
    Object.entries(LANGS).map(([k, n]) => `<option value="${k}" ${c.language === k ? "selected" : ""}>${esc(n)}</option>`).join("");
  m.innerHTML = `<div class="main-inner">
    <div class="page-head"><div class="grow"><h1>${esc(tr("Einstellungen"))}</h1><div class="sub">CraftKit ${esc(S.state.version)} · ${trh("Daten in {0}", `<span class="mono">${esc(S.state.dataDir)}</span>`)}</div></div>
      <div class="actions"><button class="btn btn-sm" id="chkUpd">${esc(tr("Nach Updates suchen"))}</button></div></div>
    <div class="card card-pad" style="margin-bottom:14px"><div class="form-grid">
      <div class="field"><label>🌐 ${esc(tr("Sprache"))}${LANG !== "en" ? " · Language" : ""}</label><select class="input" id="lang">${langOpts}</select>
        <span class="hint">${esc(tr("Die Übersetzungen sind automatisch erstellt und können Fehler enthalten."))}</span></div>
      <div class="field"><label>${esc(tr("Fehler in der Übersetzung gefunden?"))}</label>
        <div><button class="btn" id="trReport">✎ ${esc(tr("Übersetzungsfehler melden"))}</button></div>
        <span class="hint">${esc(tr("Öffnet GitHub – dort kannst du beschreiben, was falsch ist (kostenloses Konto nötig)."))}</span></div>
    </div></div>
    <div class="card card-pad"><div class="form-grid">
      <div class="field" style="grid-column:1/-1"><label>${esc(tr("CurseForge-API-Key"))}</label><input class="input mono" id="cfKey" type="password" value="${esc(c.curseforgeKey)}" autocomplete="off">
        <span class="hint">${trh("Nötig für Suche und Downloads über CurseForge. Einen eigenen Key gibt es kostenlos auf {0}.", `<a href="https://console.curseforge.com/" data-ext>console.curseforge.com</a>`)}</span></div>
      <div class="field"><label>${esc(tr("Minecraft-Ordner"))}</label><div style="display:flex;gap:8px"><input class="input mono" id="mcDir" style="flex:1" value="${esc(c.minecraftDir)}"><button class="btn" data-pick="mcDir">…</button></div>
        <span class="hint">${esc(tr("Hier liegen Versionen und launcher_profiles.json."))}</span></div>
      <div class="field"><label>${esc(tr("Ordner für Instanzen"))}</label><div style="display:flex;gap:8px"><input class="input mono" id="instDir" style="flex:1" value="${esc(c.instancesDir)}"><button class="btn" data-pick="instDir">…</button></div>
        <span class="hint">${esc(tr("Pro Instanz ein Unterordner mit mods, saves, config …"))}</span></div>
      <div class="field"><label>${esc(tr("Minecraft Launcher (optional)"))}</label><input class="input mono" id="lPath" value="${esc(c.launcherPath)}" placeholder="${esc(tr("automatisch erkennen"))}">
        <span class="hint">${esc(tr("Erkannt: {0}.", S.state.launcherLabel || tr("nichts gefunden")))} ${esc(tr("Pfad zur MinecraftLauncher.exe nur angeben, wenn der Button nicht funktioniert."))}</span></div>
      <div class="field"><label>${esc(tr("Java für Forge/NeoForge-Installer (optional)"))}</label><input class="input mono" id="jPath" value="${esc(c.javaPath)}" placeholder="${esc(tr("automatisch"))}">
        <span class="hint">${esc(S.state.java ? tr("Gefunden: {0}", S.state.java) : tr("Kein Java gefunden – wird bei Bedarf automatisch geladen."))}</span></div>
      <label class="check"><input type="checkbox" id="snap" ${c.showSnapshots ? "checked" : ""}> ${esc(tr("Snapshots standardmäßig anzeigen"))}</label>
      <label class="check"><input type="checkbox" id="autoBk" ${c.autoBackupWorlds !== false ? "checked" : ""}> ${esc(tr("Welten vor Mod-Updates automatisch sichern"))}</label>
    </div>
    <div style="display:flex;justify-content:flex-end;margin-top:16px"><button class="btn btn-primary" id="cSave">${esc(tr("Speichern"))}</button></div></div></div>`;
  $("#chkUpd").onclick = () => checkForUpdate(true);
  $("#trReport").onclick = reportTranslation;
  $("#lang").onchange = async () => {
    const v = $("#lang").value;
    try {
      await api("/api/config", { ...configPayload(), language: v });
      await refreshState();
      await loadLang(detectLang(v));
      render();
    } catch (e) { toast(e.message, true); }
  };
  $$("[data-pick]").forEach(b => b.onclick = async () => {
    try { const r = await api("/api/pick-folder", { title: tr("Ordner wählen") }); if (r.path) $("#" + b.dataset.pick).value = r.path; }
    catch (e) { toast(e.message, true); }
  });
  $("#cSave").onclick = async () => {
    try {
      await api("/api/config", configPayload());
      await refreshState();
      toast(tr("Gespeichert."));
      renderSettings(m);
    } catch (e) { toast(e.message, true); }
  };
}

// configPayload collects the settings form (the language is kept as it is unless given).
function configPayload() {
  return { minecraftDir: $("#mcDir").value, instancesDir: $("#instDir").value, launcherPath: $("#lPath").value,
    curseforgeKey: $("#cfKey").value, javaPath: $("#jPath").value, showSnapshots: $("#snap").checked, autoBackupWorlds: $("#autoBk").checked,
    language: S.state.config.language || "" };
}

// ---------- self update ----------
async function checkForUpdate(manual = false) {
  let u;
  try { u = await api("/api/update/check"); }
  catch (e) { if (manual) toast(tr("Update-Prüfung fehlgeschlagen: {0}", e.message), true); return; }
  S.update = u;
  renderTopStatus();
  if (manual) {
    if (u.available) updateModal();
    else toast(u.current === "dev" ? tr("Entwicklerversion – keine Update-Prüfung.") : tr("CraftKit {0} ist aktuell.", u.current));
  }
}

function updateModal() {
  const u = S.update;
  const md = modal(`<div class="modal-head"><div style="flex:1"><h2>⬆ ${esc(tr("CraftKit {0} ist verfügbar", u.latest))}</h2>
      <div class="sub">${esc(tr("Du hast Version {0}. Das Update wird heruntergeladen, geprüft und CraftKit startet neu – deine Instanzen und Einstellungen bleiben erhalten.", u.current))}</div></div></div>
    <div class="modal-body">${u.notes ? `<div class="section-label" style="margin-top:0">${esc(tr("Was ist neu"))}</div><div class="card card-pad" style="white-space:pre-wrap;font-size:13px;max-height:260px;overflow:auto">${esc(u.notes)}</div>` : ""}
      ${u.url ? `<div style="margin-top:10px"><a href="${esc(u.url)}" data-ext>${esc(tr("Auf GitHub ansehen"))}</a></div>` : ""}</div>
    <div class="modal-foot"><button class="btn btn-ghost" data-close>${esc(tr("Später"))}</button><button class="btn btn-primary" id="upGo">${esc(tr("Jetzt aktualisieren"))}</button></div>`);
  $("#upGo", md).onclick = async () => {
    if (anyRunning()) return toast(tr("Bitte warte, bis die laufende Installation fertig ist."), true);
    try {
      const { job } = await api("/api/update/apply", {});
      closeModal(md);
      jobModal(job, tr("CraftKit {0} wird installiert", u.latest), async () => { restartWait(); });
    } catch (e) { toast(e.message, true); }
  };
}

function anyRunning() { return !!$(".progress.indet") || false; }

function restartWait() {
  const back = document.createElement("div");
  back.className = "modal-back";
  back.innerHTML = `<div class="modal" style="width:min(420px,100%)"><div class="modal-body" style="padding:28px;text-align:center">
    <div class="spinner" style="width:28px;height:28px"></div><h2 style="margin:14px 0 4px">${esc(tr("CraftKit startet neu …"))}</h2><div class="muted">${esc(tr("Einen Moment, das Fenster lädt gleich neu."))}</div></div></div>`;
  $$("#modalRoot .modal-back").forEach(x => x.remove());
  $("#modalRoot").appendChild(back);
  const started = Date.now();
  const tryReload = async () => {
    if (Date.now() - started > 3000) {
      try {
        const r = await fetch("/api/hello", { cache: "no-store" });
        if (r.ok) { location.reload(); return; }
      } catch {}
    }
    if (Date.now() - started > 60000) { back.querySelector(".muted").textContent = tr("Bitte CraftKit neu öffnen."); return; }
    setTimeout(tryReload, 1000);
  };
  setTimeout(tryReload, 1000);
}

// ---------- launcher ----------
async function openLauncher() {
  const instanceId = S.view.type === "instance" ? S.view.id : "";
  try {
    const r = await api("/api/open-launcher", { instanceId });
    if (r.profile) toast(r.movedToTop ? tr("Minecraft Launcher wird geöffnet – wähle dort das Profil „{0}“ (steht ganz oben).", r.profile) : tr("Minecraft Launcher wird geöffnet – wähle dort das Profil „{0}“.", r.profile));
    else if ((r.profiles || []).length === 1) toast(tr("Minecraft Launcher wird geöffnet – dein CraftKit-Profil heißt „{0}“.", r.profiles[0]));
    else if ((r.profiles || []).length) toast(tr("Minecraft Launcher wird geöffnet. Deine CraftKit-Profile: {0}.", r.profiles.map(n => "„" + n + "“").join(", ")));
    else toast(tr("Minecraft Launcher wird geöffnet."));
  } catch (e) { toast(tr("Launcher konnte nicht geöffnet werden: {0}", e.message), true); }
}

// ---------- boot ----------
$("#btnLauncher").onclick = openLauncher;
$("#btnNewInstance").onclick = () => go({ type: "new" });
$("#btnNewPluginFolder").onclick = () => go({ type: "newPlugins" });
$("#btnSettings").onclick = () => go({ type: "settings" });
setInterval(() => refreshState().catch(() => {}), 30000);
refreshState().then(async () => { await loadLang(detectLang(S.state.config.language)); go({ type: "welcome" }); checkForUpdate(false); }).catch(e => {
  $("#main").innerHTML = `<div class="main-inner"><div class="banner banner-err"><span class="b-ico">✕</span><div>${esc(e.message)}</div></div></div>`;
});
