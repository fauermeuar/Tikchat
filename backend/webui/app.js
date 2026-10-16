"use strict";

const $ = (id) => document.getElementById(id);
const feed = $("feed");
const userIn = $("username");

const MAX_ROWS = 300;
const NAME_RE = /^[A-Za-z0-9._]{1,24}$/;
const ICON_X = '<svg viewBox="0 0 24 24" width="14" height="14" fill="none" stroke="currentColor" stroke-width="2.4" stroke-linecap="round" aria-hidden="true"><path d="M6 6l12 12M18 6 6 18"/></svg>';

let lastSeq = 0;          // seq komentar terakhir yang sudah tampil
let sessionStart = 0;     // started_at sesi pantau yang sedang ditampilkan
let stick = true;         // feed menempel di bawah (auto-scroll)
let unread = 0;           // komentar baru saat user membaca ke atas
let filterText = "";
let failCount = 0;
let pollTimer = 0;
let busy = false;
let savedNames = [];
let watchUser = "";

/* ---------- util ---------- */
const store = {
  get(k) { try { return localStorage.getItem(k); } catch (e) { return null; } },
  set(k, v) { try { localStorage.setItem(k, v); } catch (e) { /* diabaikan */ } },
};

async function api(path, method, body) {
  const r = await fetch(path, {
    method: method || "GET",
    headers: body ? { "Content-Type": "application/json" } : {},
    body: body ? JSON.stringify(body) : undefined,
    cache: "no-store",
  });
  const txt = await r.text();
  try {
    return JSON.parse(txt);
  } catch (e) {
    throw new Error(txt.trim() || "HTTP " + r.status);
  }
}

function toast(msg, kind) {
  const t = document.createElement("div");
  t.className = "toast " + (kind || "");
  t.textContent = msg;
  $("toasts").appendChild(t);
  setTimeout(() => t.remove(), 2800);
}

function ask(title, msg, okText) {
  const dlg = $("dlg");
  if (!dlg || typeof dlg.showModal !== "function") {
    return Promise.resolve(window.confirm(title + "\n" + msg));
  }
  $("dlgTitle").textContent = title;
  $("dlgMsg").textContent = msg;
  $("dlgOk").textContent = okText || "Ya";
  dlg.returnValue = "";
  return new Promise((resolve) => {
    dlg.addEventListener("close", () => resolve(dlg.returnValue === "ok"), { once: true });
    dlg.showModal();
  });
}

const fmt = (n) => Number(n || 0).toLocaleString("id-ID");
function jam(ts) {
  return new Date(ts * 1000).toLocaleTimeString("id-ID", { hour: "2-digit", minute: "2-digit", second: "2-digit" });
}
function durasi(sec) {
  sec = Math.max(0, Math.floor(sec));
  const h = Math.floor(sec / 3600), m = Math.floor((sec % 3600) / 60), s = sec % 60;
  const p = (x) => String(x).padStart(2, "0");
  return h ? h + ":" + p(m) + ":" + p(s) : p(m) + ":" + p(s);
}
function cleanName(v) {
  return String(v || "").trim().replace(/^@+/, "");
}
function hueOf(s) {
  let h = 0;
  for (let i = 0; i < s.length; i++) h = (h * 31 + s.charCodeAt(i)) >>> 0;
  return h % 360;
}

/* ---------- koneksi ke daemon ---------- */
function setOnline(ok) {
  const c = $("conn");
  c.className = "chip " + (ok ? "chip-ok" : "chip-bad");
  $("connTxt").textContent = ok ? "Terhubung" : "Daemon tak terjangkau";
}

/* ---------- status ---------- */
function renderStatus(st) {
  const card = $("statusCard");
  watchUser = st.username || "";
  if (!st.username) {
    card.hidden = true;
    renderSaved();
    return;
  }
  card.hidden = false;
  $("stUser").textContent = "@" + st.username;

  const pill = $("livePill");
  if (st.live) {
    pill.className = "pill on";
    $("livePillTxt").textContent = "LIVE";
  } else if (st.err && !/mencoba lagi/.test(st.err)) {
    pill.className = "pill off";
    $("livePillTxt").textContent = st.err === "live selesai" ? "SELESAI" : "OFFLINE";
  } else {
    pill.className = "pill wait";
    $("livePillTxt").textContent = st.err ? "MENYAMBUNG" : "MENGECEK";
  }

  $("stViewers").textContent = fmt(st.viewers);
  $("stComments").textContent = fmt(st.comments);
  $("stTime").textContent = st.started_at ? durasi(Date.now() / 1000 - st.started_at) : "00:00";

  const e = $("stErr");
  if (st.err) {
    e.textContent = st.err;
    e.className = "notice" + (st.live ? "" : " bad");
    e.hidden = false;
  } else {
    e.hidden = true;
  }
  renderSaved();
}

/* ---------- feed ---------- */
function atBottom() {
  return feed.scrollHeight - feed.scrollTop - feed.clientHeight < 60;
}
function showEmpty() {
  if (feed.children.length) return;
  const d = document.createElement("div");
  d.className = "empty";
  d.innerHTML = "Belum ada komentar.<br>Masukkan username lalu tekan Pantau.";
  feed.appendChild(d);
}
function updateJump() {
  const b = $("btnJump");
  b.hidden = stick || unread === 0;
  b.textContent = "↓ " + unread + " komentar baru";
}
function matches(el) {
  return !filterText || el.dataset.q.indexOf(filterText) !== -1;
}

function makeComment(c) {
  let nick = c.user, handle = "";
  const m = /^(.*) \(@(.+)\)$/.exec(c.user || "");
  if (m) { nick = m[1]; handle = "@" + m[2]; }
  nick = nick || "?";

  const row = document.createElement("div");
  row.className = "cmt";
  row.dataset.q = (nick + " " + handle + " " + c.text).toLowerCase();
  row.hidden = !matches(row);

  const av = document.createElement("div");
  av.className = "av";
  av.style.setProperty("--h", hueOf(handle || nick));
  const first = Array.from(nick.replace(/^[^\p{L}\p{N}]+/u, ""))[0] || "?";
  av.textContent = first.toUpperCase();

  const body = document.createElement("div");
  body.className = "cmt-body";
  const top = document.createElement("div");
  top.className = "cmt-top";
  const n = document.createElement("span");
  n.className = "cmt-name";
  n.textContent = nick;
  top.appendChild(n);
  if (handle) {
    const h = document.createElement("span");
    h.className = "cmt-handle";
    h.textContent = handle;
    top.appendChild(h);
  }
  const t = document.createElement("span");
  t.className = "cmt-time";
  t.textContent = jam(c.ts);
  top.appendChild(t);
  const x = document.createElement("div");
  x.className = "cmt-text";
  x.textContent = c.text;
  body.append(top, x);
  row.append(av, body);
  return row;
}

function addComments(list) {
  if (!list || !list.length) return;
  const empty = feed.querySelector(".empty");
  if (empty) empty.remove();
  const wasBottom = stick;
  const frag = document.createDocumentFragment();
  let added = 0;
  const fresh = [];
  for (const c of list) {
    if (c.seq <= lastSeq) continue;
    lastSeq = c.seq;
    frag.appendChild(makeComment(c));
    fresh.push(c);
    added++;
  }
  if (!added) return;
  feed.appendChild(frag);
  while (feed.children.length > MAX_ROWS) feed.removeChild(feed.firstChild);
  if (wasBottom) {
    feed.scrollTop = feed.scrollHeight;
  } else {
    unread += added;
    updateJump();
  }
  $("feedInfo").textContent = "(" + fmt(lastSeq) + " total)";
  // mode bacakan komentar: suarakan yang baru (maks 3 per batch biar wajar)
  if (readAloud) {
    const tail = fresh.slice(-3);
    for (const c of tail) speakComment(c);
  }
}

function resetFeed() {
  lastSeq = 0; unread = 0; stick = true;
  feed.innerHTML = "";
  $("feedInfo").textContent = "";
  showEmpty();
  updateJump();
}

feed.addEventListener("scroll", () => {
  stick = atBottom();
  if (stick) unread = 0;
  updateJump();
}, { passive: true });
$("btnJump").onclick = () => {
  feed.scrollTop = feed.scrollHeight;
  stick = true; unread = 0; updateJump();
};
$("btnClear").onclick = async () => {
  const ok = await ask(
    "Bersihkan tampilan?",
    "Komentar yang tampil akan dihapus dari layar. Komentar baru tetap masuk seperti biasa.",
    "Ya, bersihkan"
  );
  if (!ok) return;
  const keep = lastSeq;
  resetFeed();
  lastSeq = keep; // jangan tarik ulang komentar lama dari buffer daemon
  toast("Tampilan dibersihkan");
};
$("filter").addEventListener("input", (e) => {
  filterText = e.target.value.trim().toLowerCase();
  for (const el of feed.children) {
    if (el.dataset && el.dataset.q !== undefined) el.hidden = !matches(el);
  }
});

/* ---------- polling ---------- */
async function poll() {
  clearTimeout(pollTimer);
  let delay = document.hidden ? 5000 : 1500;
  try {
    const s = await api("/api/status");
    failCount = 0;
    setOnline(true);
    if (s.version) $("ver").textContent = s.version;
    const w = s.watch || {};
    // sesi pantau baru (atau daemon restart): kosongkan feed lama
    if (w.username && (w.started_at !== sessionStart || w.comments < lastSeq)) {
      if (sessionStart !== 0 || w.comments < lastSeq) resetFeed();
      sessionStart = w.started_at;
    }
    if (!w.username) sessionStart = 0;
    renderStatus(w);
    if (w.username && w.comments > lastSeq) {
      const c = await api("/api/comments?after=" + lastSeq);
      addComments(c.comments);
    }
  } catch (e) {
    failCount++;
    setOnline(false);
    delay = Math.min(10000, 1500 * (failCount + 1));
  }
  pollTimer = setTimeout(poll, delay);
}
document.addEventListener("visibilitychange", () => { if (!document.hidden) poll(); });

/* ---------- aksi ---------- */
function setBusy(on) {
  busy = on;
  $("btnWatch").disabled = on;
  $("btnWatchTxt").textContent = on ? "Menghubungkan…" : "Pantau";
}

async function startWatch(name) {
  const u = cleanName(name);
  if (busy) return;
  if (!u) { toast("Isi username dulu", "err"); userIn.focus(); return; }
  if (!NAME_RE.test(u)) { toast("Username hanya huruf, angka, titik, dan garis bawah (maks 24)", "err"); return; }
  userIn.value = u;
  store.set("tikchat.last", u);
  setBusy(true);
  try {
    const r = await api("/api/watch", "POST", { username: u });
    if (r.ok) {
      resetFeed();
      sessionStart = r.watch ? r.watch.started_at : 0;
      toast("Memantau @" + u, "ok");
      logMsg("pantau: @" + u);
      poll();
    } else {
      toast(r.err || "Gagal memulai pantauan", "err");
    }
  } catch (e) {
    toast("Gagal: " + e.message, "err");
  } finally {
    setBusy(false);
  }
}

$("btnWatch").onclick = () => startWatch(userIn.value);
userIn.addEventListener("keydown", (e) => { if (e.key === "Enter") startWatch(userIn.value); });

$("btnStop").onclick = async () => {
  const ok = await ask("Berhenti memantau?", "@" + watchUser + " tidak akan dipantau lagi.", "Stop");
  if (!ok) return;
  try {
    await api("/api/stop", "POST");
    toast("Pantauan dihentikan");
    logMsg("pantau: berhenti");
    poll();
  } catch (e) {
    toast("Gagal: " + e.message, "err");
  }
};

/* ---------- username tersimpan ---------- */
function renderSaved() {
  const box = $("savedList");
  $("savedCount").textContent = savedNames.length ? savedNames.length + " akun" : "";
  box.innerHTML = "";
  if (!savedNames.length) {
    const d = document.createElement("div");
    d.className = "empty";
    d.innerHTML = "Belum ada.<br>Ketik username di atas lalu tekan Simpan.";
    box.appendChild(d);
    return;
  }
  for (const u of savedNames) {
    const chip = document.createElement("div");
    chip.className = "chip-user" + (u.toLowerCase() === watchUser.toLowerCase() ? " active" : "");
    const go = document.createElement("button");
    go.type = "button";
    go.className = "go";
    go.textContent = "@" + u;
    go.title = "Pantau @" + u;
    go.onclick = () => startWatch(u);
    const rm = document.createElement("button");
    rm.type = "button";
    rm.className = "rm";
    rm.title = "Hapus @" + u;
    rm.setAttribute("aria-label", "Hapus @" + u);
    rm.innerHTML = ICON_X;
    rm.onclick = async () => {
      if (!(await ask("Hapus dari daftar?", "@" + u + " akan dihapus dari username tersimpan.", "Hapus"))) return;
      try {
        const r = await api("/api/usernames?username=" + encodeURIComponent(u), "DELETE");
        savedNames = r.usernames || [];
        renderSaved();
      } catch (e) {
        toast("Gagal: " + e.message, "err");
      }
    };
    chip.append(go, rm);
    box.appendChild(chip);
  }
}

async function loadSaved() {
  try {
    const r = await api("/api/usernames");
    savedNames = r.usernames || [];
    renderSaved();
  } catch (e) {
    setTimeout(loadSaved, 3000); // daemon mungkin masih mulai
  }
}

$("btnSave").onclick = async () => {
  const u = cleanName(userIn.value);
  if (!u) { toast("Isi username dulu", "err"); userIn.focus(); return; }
  if (!NAME_RE.test(u)) { toast("Username tidak valid", "err"); return; }
  try {
    const before = savedNames.length;
    const r = await api("/api/usernames", "POST", { username: u });
    if (!r.ok) { toast(r.err || "Gagal menyimpan", "err"); return; }
    savedNames = r.usernames || [];
    renderSaved();
    toast(savedNames.length > before ? "@" + u + " disimpan" : "@" + u + " sudah ada di daftar", savedNames.length > before ? "ok" : "");
  } catch (e) {
    toast("Gagal: " + e.message, "err");
  }
};

/* ---------- suara: TTS browser atau bridge Android (WebView APK) ---------- */
let readAloud = store.get("tikchat.readaloud") === "1";
if (store.get("tikchat.readaloud") === null) {
  // migrasi dari mode suara v1.2.2
  const vm = store.get("tikchat.voiceMode");
  readAloud = vm === "comments" || vm === "all";
}

// Volume 0..1 (disimpan 0..100 agar sinkron dengan slider).
let volTts = Math.max(0, Math.min(100, parseInt(store.get("tikchat.volTts") || "100", 10) || 0)) / 100;
let volMus = Math.max(0, Math.min(100, parseInt(store.get("tikchat.volMus") || "80", 10) || 0)) / 100;
let volMic = Math.max(0, Math.min(100, parseInt(store.get("tikchat.volMic") || "80", 10) || 0)) / 100;

// TTS: abstraksi di atas speechSynthesis (browser) atau window.AndroidTTS
// (bridge native di APK — WebView tidak punya speechSynthesis).
const TTS = {
  mode: null,   // 'web' | 'android'
  voices: [],   // [{id, name, lang}]
  init() {
    if (window.AndroidTTS) {
      this.mode = "android";
      try {
        const arr = JSON.parse(window.AndroidTTS.getVoices() || "[]");
        this.voices = Array.isArray(arr) ? arr.map((v) => ({
          id: String(v.id || v.name || ""),
          name: String(v.name || v.id || "?"),
          lang: String(v.lang || ""),
        })) : [];
      } catch (e) { this.voices = []; }
      this.sortVoices();
      return;
    }
    if ("speechSynthesis" in window) {
      this.mode = "web";
      this.refreshWeb();
      try {
        speechSynthesis.onvoiceschanged = () => { this.refreshWeb(); fillVoiceList(); };
      } catch (e) {}
    }
  },
  refreshWeb() {
    let vs = [];
    try { vs = speechSynthesis.getVoices() || []; } catch (e) {}
    this.voices = vs.map((v) => ({ id: v.voiceURI, name: v.name, lang: v.lang, ref: v }));
    this.sortVoices();
  },
  sortVoices() {
    const idScore = (l) => (/^(id|in)\b/i.test(l || "") ? 0 : 1);
    this.voices.sort((a, b) =>
      (idScore(a.lang) - idScore(b.lang)) || String(a.name).localeCompare(String(b.name)));
  },
  pick() {
    const want = store.get("tikchat.voiceURI") || "";
    return this.voices.find((v) => v.id === want) || this.voices[0] || null;
  },
  speak(text) {
    const t = String(text || "").trim();
    if (!t) return;
    if (this.mode === "android") {
      try {
        const v = this.pick();
        window.AndroidTTS.speak(t, v ? v.id : "", volTts);
      } catch (e) {}
      return;
    }
    if (this.mode === "web") {
      try {
        const u = new SpeechSynthesisUtterance(t);
        u.lang = "id-ID";
        u.volume = volTts;
        const v = this.pick();
        if (v && v.ref) u.voice = v.ref;
        speechSynthesis.speak(u);
      } catch (e) {}
    }
  },
  cancel() {
    try {
      if (this.mode === "android") window.AndroidTTS.cancel();
      else if (this.mode === "web") speechSynthesis.cancel();
    } catch (e) {}
  },
  get pending() {
    try {
      if (this.mode === "android") return !!window.AndroidTTS.isSpeaking();
      if (this.mode === "web") return speechSynthesis.pending;
    } catch (e) {}
    return false;
  },
};

// fillVoiceList: isi dropdown dengan suara yang tersedia di HP
// (Indonesia di atas). Browser memuat daftar async -> refresh saat ready.
function fillVoiceList() {
  const sel = $("voicePick");
  if (!sel) return;
  const cur = store.get("tikchat.voiceURI") || "";
  sel.innerHTML = "";
  const vs = TTS.voices;
  if (!vs.length) {
    const o = document.createElement("option");
    o.value = "";
    o.textContent = "🔊 Suara HP";
    sel.appendChild(o);
    sel.disabled = true;
    return;
  }
  sel.disabled = false;
  for (const v of vs) {
    const o = document.createElement("option");
    o.value = v.id;
    o.textContent = v.name + (v.lang ? " (" + v.lang + ")" : "");
    sel.appendChild(o);
  }
  const found = vs.some((v) => v.id === cur);
  sel.value = found ? cur : vs[0].id;
  if (!found) store.set("tikchat.voiceURI", sel.value);
}

function speak(text) {
  TTS.speak(text);
}

function renderVoice() {
  $("voiceTxt").textContent = readAloud ? "Nyala" : "Mati";
}

$("btnVoice").onclick = () => {
  readAloud = !readAloud;
  store.set("tikchat.readaloud", readAloud ? "1" : "0");
  renderVoice();
  TTS.cancel();
  if (readAloud) {
    speak("Pembaca komentar aktif");
    toast("Bacakan komentar nyala", "ok");
    logMsg("tts: pembaca komentar nyala");
  } else {
    logMsg("tts: pembaca komentar mati");
  }
};

$("voicePick").addEventListener("change", (e) => {
  store.set("tikchat.voiceURI", e.target.value);
  TTS.cancel();
  speak("Ini contoh suaranya");
});

// speakComment: bacakan satu komentar.
// Anti-menumpuk: bila masih ada yang antre, lewati — live yang ramai tidak
// boleh bikin antrean suara sepanjang kereta.
function speakComment(c) {
  const t = String(c.text || "").trim();
  if (t.length < 3) return;
  if (TTS.pending) return;
  speak(t.length > 180 ? t.slice(0, 180) : t);
}

/* ---------- v1.4.9: tab ---------- */
function switchTab(name) {
  const audio = name === "audio";
  $("viewAudio").hidden = !audio;
  $("viewKomentar").hidden = audio;
  $("tabAudio").classList.toggle("active", audio);
  $("tabKomentar").classList.toggle("active", !audio);
  store.set("tikchat.tab", name);
  window.scrollTo(0, 0);
}
$("tabKomentar").onclick = () => switchTab("komentar");
$("tabAudio").onclick = () => switchTab("audio");

/* ---------- v1.4.9: log ---------- */
const LOG_MAX = 200;
function logMsg(msg) {
  const box = $("logBox");
  if (!box) return;
  const d = document.createElement("div");
  d.className = "log-line";
  const t = new Date().toLocaleTimeString("id-ID", { hour: "2-digit", minute: "2-digit", second: "2-digit" });
  d.textContent = "[" + t + "] " + msg;
  box.appendChild(d);
  while (box.children.length > LOG_MAX) box.removeChild(box.firstChild);
  box.scrollTop = box.scrollHeight;
}
$("btnLogClear").onclick = () => { $("logBox").innerHTML = ""; };

/* ---------- v1.4.9: musik (MP3 dari HP, tersimpan di IndexedDB) ---------- */
const Music = {
  songs: [], idx: -1, db: null, audio: null, objUrl: "",
  init() {
    this.audio = new Audio();
    this.audio.preload = "none";
    this.audio.volume = volMus;
    this.audio.addEventListener("ended", () => this.next());
    this.audio.addEventListener("play", () => this.render());
    this.audio.addEventListener("pause", () => this.render());
    this.audio.addEventListener("error", () => {
      const s = this.songs[this.idx];
      if (s) { logMsg("musik: gagal putar " + s.name); toast("Gagal putar: " + s.name, "err"); }
    });
    $("btnPlay").onclick = () => this.toggle();
    $("btnNext").onclick = () => this.next();
    $("btnPrev").onclick = () => this.prev();
    $("btnPickOne").onclick = () => $("filePick").click();
    $("btnPickMany").onclick = () => $("filePickMany").click();
    $("filePick").addEventListener("change", (e) => { this.addFiles(e.target.files); e.target.value = ""; });
    $("filePickMany").addEventListener("change", (e) => { this.addFiles(e.target.files); e.target.value = ""; });
    $("btnMusicClear").onclick = async () => {
      if (!this.songs.length) return;
      if (!(await ask("Hapus semua lagu?", this.songs.length + " lagu tersimpan akan dihapus dari browser.", "Hapus"))) return;
      this.stop();
      this.songs = []; this.idx = -1;
      try {
        await new Promise((res, rej) => {
          const tx = this.db.transaction("songs", "readwrite");
          tx.objectStore("songs").clear();
          tx.oncomplete = res; tx.onerror = () => rej(tx.error);
        });
      } catch (e) {}
      this.render();
      logMsg("musik: semua lagu dihapus");
      toast("Semua lagu dihapus");
    };
    this.openDB().then(() => this.loadAll()).catch(() => logMsg("musik: IndexedDB tak tersedia"));
  },
  openDB() {
    return new Promise((resolve, reject) => {
      if (!("indexedDB" in window)) return reject(new Error("no idb"));
      const rq = indexedDB.open("tikchat-db", 1);
      rq.onupgradeneeded = () => { rq.result.createObjectStore("songs", { keyPath: "id" }); };
      rq.onsuccess = () => { this.db = rq.result; resolve(); };
      rq.onerror = () => reject(rq.error);
    });
  },
  loadAll() {
    return new Promise((resolve) => {
      const out = [];
      try {
        const tx = this.db.transaction("songs", "readonly");
        const rq = tx.objectStore("songs").openCursor();
        rq.onsuccess = () => {
          const cur = rq.result;
          if (cur) { out.push({ id: cur.value.id, name: cur.value.name, type: cur.value.type }); cur.continue(); }
          else { this.songs = out; this.render(); resolve(); }
        };
        rq.onerror = () => { this.songs = []; this.render(); resolve(); };
      } catch (e) { this.songs = []; this.render(); resolve(); }
    });
  },
  getData(id) {
    return new Promise((resolve, reject) => {
      try {
        const tx = this.db.transaction("songs", "readonly");
        const rq = tx.objectStore("songs").get(id);
        rq.onsuccess = () => resolve(rq.result);
        rq.onerror = () => reject(rq.error);
      } catch (e) { reject(e); }
    });
  },
  async addFiles(files) {
    if (!files || !files.length) return;
    if (!this.db) { toast("Penyimpanan lagu belum siap", "err"); return; }
    let n = 0;
    for (const f of files) {
      try {
        const data = await f.arrayBuffer();
        const rec = {
          id: "s" + Date.now() + Math.floor(Math.random() * 1e6),
          name: f.name, type: f.type || "audio/mpeg", data,
        };
        await new Promise((res, rej) => {
          const tx = this.db.transaction("songs", "readwrite");
          tx.objectStore("songs").put(rec);
          tx.oncomplete = res; tx.onerror = () => rej(tx.error);
        });
        n++;
      } catch (e) { /* lewati file yang gagal */ }
    }
    if (n) {
      await this.loadAll();
      logMsg("musik: " + n + " lagu ditambahkan");
      toast(n + " lagu ditambahkan", "ok");
    }
  },
  async removeSong(id) {
    this.stop();
    this.songs = this.songs.filter((s) => s.id !== id);
    if (this.idx >= this.songs.length) this.idx = this.songs.length - 1;
    try {
      await new Promise((res, rej) => {
        const tx = this.db.transaction("songs", "readwrite");
        tx.objectStore("songs").delete(id);
        tx.oncomplete = res; tx.onerror = () => rej(tx.error);
      });
    } catch (e) {}
    this.render();
    logMsg("musik: 1 lagu dihapus");
  },
  cur() { return this.songs[this.idx] || null; },
  async play(i) {
    if (!this.songs.length) return;
    this.idx = ((i % this.songs.length) + this.songs.length) % this.songs.length;
    const meta = this.songs[this.idx];
    try {
      const rec = await this.getData(meta.id);
      if (!rec) throw new Error("data hilang");
      if (this.objUrl) URL.revokeObjectURL(this.objUrl);
      this.objUrl = URL.createObjectURL(new Blob([rec.data], { type: rec.type || "audio/mpeg" }));
      this.audio.src = this.objUrl;
      await this.audio.play();
      logMsg("musik: memutar " + meta.name);
    } catch (e) {
      logMsg("musik: gagal putar " + meta.name);
      toast("Gagal memutar lagu", "err");
    }
    this.render();
  },
  toggle() {
    if (!this.songs.length) { toast("Belum ada lagu — pilih MP3 dulu"); return; }
    if (this.audio.paused) {
      if (!this.audio.src && this.idx < 0) this.play(0);
      else this.audio.play().catch(() => {});
    } else {
      this.audio.pause();
    }
  },
  next() { if (this.songs.length) this.play(this.idx + 1); },
  prev() { if (this.songs.length) this.play(this.idx - 1); },
  stop() {
    try { this.audio.pause(); } catch (e) {}
    this.audio.removeAttribute("src");
    this.audio.load();
  },
  setVolume(v) { if (this.audio) this.audio.volume = v; },
  render() {
    const meta = this.cur();
    $("songTitle").textContent = meta ? meta.name : "Belum ada lagu";
    $("btnPlay").textContent = this.audio && !this.audio.paused ? "⏸" : "▶";
    const box = $("songList");
    box.innerHTML = "";
    this.songs.forEach((s, i) => {
      const row = document.createElement("div");
      row.className = "song" + (i === this.idx ? " active" : "");
      const nm = document.createElement("span");
      nm.className = "nm";
      nm.textContent = s.name;
      nm.title = s.name;
      const st = document.createElement("span");
      st.className = "st";
      st.textContent = (i === this.idx && this.audio && !this.audio.paused) ? "▶" : "";
      const del = document.createElement("button");
      del.type = "button";
      del.className = "del";
      del.textContent = "✕";
      del.title = "Hapus lagu ini";
      del.setAttribute("aria-label", "Hapus " + s.name);
      del.onclick = (e) => { e.stopPropagation(); this.removeSong(s.id); };
      row.append(nm, st, del);
      row.onclick = () => this.play(i);
      box.appendChild(row);
    });
  },
};

/* ---------- v1.4.9: mikrofon -> speaker ---------- */
const Mic = {
  on: false, stream: null, ctx: null, gain: null,
  async toggle() {
    if (this.on) { this.stop(); return; }
    await this.start();
  },
  async start() {
    if (!navigator.mediaDevices || !navigator.mediaDevices.getUserMedia) {
      toast("Mic tidak didukung di browser ini", "err");
      return;
    }
    try {
      this.stream = await navigator.mediaDevices.getUserMedia({
        audio: { echoCancellation: true, noiseSuppression: true, autoGainControl: true },
      });
    } catch (e) {
      const m = (e && e.message) ? e.message : String(e);
      toast("Mic gagal: " + m, "err");
      logMsg("mic: gagal (" + m + ")");
      return;
    }
    try {
      const AC = window.AudioContext || window.webkitAudioContext;
      this.ctx = new AC();
      if (this.ctx.state === "suspended") { try { await this.ctx.resume(); } catch (e2) {} }
      const src = this.ctx.createMediaStreamSource(this.stream);
      this.gain = this.ctx.createGain();
      this.gain.gain.value = volMic;
      src.connect(this.gain);
      this.gain.connect(this.ctx.destination);
    } catch (e) {
      try { this.stream.getTracks().forEach((t) => t.stop()); } catch (e2) {}
      this.stream = null;
      toast("Audio HP gagal disiapkan", "err");
      return;
    }
    this.on = true;
    this.render();
    logMsg("mic: nyala");
    toast("Mic nyala — suara keluar ke speaker", "ok");
  },
  stop() {
    try { if (this.stream) this.stream.getTracks().forEach((t) => t.stop()); } catch (e) {}
    try { if (this.ctx) this.ctx.close(); } catch (e) {}
    this.stream = null; this.ctx = null; this.gain = null;
    this.on = false;
    this.render();
    logMsg("mic: mati");
  },
  setVolume(v) { if (this.gain) this.gain.gain.value = v; },
  render() {
    $("micState").textContent = this.on ? "Nyala" : "Mati";
    $("btnMic").textContent = this.on ? "🎤 Matikan mic" : "🎤 Nyalakan mic";
  },
};

/* ---------- v1.4.9: volume ---------- */
function bindVolume(inputId, labelId, key, apply) {
  const el = $(inputId);
  const saved = parseInt(store.get(key) || "", 10);
  if (!isNaN(saved)) el.value = Math.max(0, Math.min(100, saved));
  const show = () => { $(labelId).textContent = el.value + "%"; };
  el.addEventListener("input", () => {
    show();
    store.set(key, String(el.value));
    apply(el.value / 100);
  });
  show();
  apply(el.value / 100);
}

/* ---------- mulai ---------- */
const last = store.get("tikchat.last");
if (last) userIn.value = last;
showEmpty();
renderSaved();
loadSaved();
renderVoice();
TTS.init();
fillVoiceList();
switchTab(store.get("tikchat.tab") === "audio" ? "audio" : "komentar");
bindVolume("volTts", "volTtsVal", "tikchat.volTts", (v) => { volTts = v; });
bindVolume("volMus", "volMusVal", "tikchat.volMus", (v) => { volMus = v; Music.setVolume(v); });
bindVolume("volMic", "volMicVal", "tikchat.volMic", (v) => { volMic = v; Mic.setVolume(v); });
$("btnMic").onclick = () => Mic.toggle();
Music.init();
Mic.render();
logMsg("audio siap");
poll();

if ("serviceWorker" in navigator) {
  window.addEventListener("load", () => navigator.serviceWorker.register("sw.js").catch(() => {}));
}
