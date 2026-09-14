/* Memory web UI — browser voice call client.
   Vanilla JS, no bundler. Uses the vendored LiveKit UMD build loaded globally
   as `window.LivekitClient` (static/js/vendor/livekit-client.umd.js).

   Lifecycle mirrors the iOS MemorySessionController: token fetch → Room
   connect → publish local mic track → attach agent audio on TrackSubscribed.
   Mute toggles micTrack.mute()/unmute() (track stays alive). The call button
   toggles start ↔ hangup; everything getUserMedia/autoplay-sensitive happens
   inside the user-gesture click handler.

   This file only WIRES elements rendered elsewhere; it never creates them:
     #voice-call-btn   button, click toggles start vs hangup
     #voice-mute-btn   button, toggles mute; hidden until connected
     #voice-status     text element, status strings written here
     #voice-level      mic level meter (drives --level + .voice-speaking)
     #agent-audio      audio element, remote agent audio target
     #chat-agent       select (option value = agent id, text = agent name) */
(function () {
  "use strict";

  /* Module-scope state: kept here so mute/hangup can reach the live track and
     room without re-acquiring them. */
  var room = null;       // LiveKit Room instance (connected)
  var micTrack = null;   // published LocalAudioTrack (stays alive across mute)
  var state = "idle";    // "idle" | "connecting" | "connected"
  var audioBlocked = false; // connected but playback needs a user gesture
  var errorMsg = null;   // sticky error text; survives the Disconnected reset

  var callBtn = null, muteBtn = null, statusEl = null, audioEl = null, levelEl = null;

  // Mic level meter (Web Audio API tap on the local mic track).
  var audioCtx = null, analyser = null, levelBuf = null, levelRaf = null;

  // Live transcription (lk.transcription text stream) — interim lines, replaced
  // or appended per chunk, committed on lk.transcription_final.
  var currentUserLine = null, currentAgentLine = null;

  var STATUS_IDLE = "idle";
  var CALL_ICON_START = "lucide--phone";
  var CALL_ICON_HANGUP = "lucide--phone-off";

  function init() {
    callBtn = document.getElementById("voice-call-btn");
    muteBtn = document.getElementById("voice-mute-btn");
    statusEl = document.getElementById("voice-status");
    audioEl = document.getElementById("agent-audio");
    levelEl = document.getElementById("voice-level");
    if (!callBtn) return; // page without the voice bar: nothing to wire

    callBtn.addEventListener("click", onCallClick);
    if (muteBtn) muteBtn.addEventListener("click", toggleMute);
    // AudioPlaybackStatusChanged may flag that a user gesture is required
    // while connected — a click on the status line also enables playback.
    if (statusEl) statusEl.addEventListener("click", onStatusClick);

    if (state === "idle") resetUI(true);
  }

  /* ---------- public-ish surface (also used internally) ---------- */

  function currentAgentName() {
    var sel = document.getElementById("chat-agent");
    if (sel && sel.selectedOptions && sel.selectedOptions.length) {
      var t = (sel.selectedOptions[0].textContent || "").trim();
      if (t) return t;
    }
    return "Memory";
  }

  function randomIdentity() {
    var buf = new Uint8Array(4);
    crypto.getRandomValues(buf);
    var hex = "";
    for (var i = 0; i < buf.length; i++) {
      hex += ("0" + buf[i].toString(16)).slice(-2);
    }
    return "memory-web-" + hex;
  }

  function setStatus(text) {
    if (statusEl) statusEl.textContent = text;
  }

  /* ---------- call button / mute button UI ---------- */

  function setCallMode(starting) {
    if (!callBtn) return;
    callBtn.classList.toggle("voice-calling", !starting);
    callBtn.setAttribute(
      "aria-label",
      starting ? "Start voice call" : "Hang up voice call"
    );
    // swap the iconify glyph if one is present (size/position classes kept)
    var icon = callBtn.querySelector(".iconify");
    if (icon) {
      var cls = icon.className || "";
      cls = cls.replace(/lucide--[a-z0-9-]+/g, "").trim();
      icon.className = ("iconify " + (starting ? CALL_ICON_START : CALL_ICON_HANGUP) + " " + cls).trim();
    }
  }

  function setMuteUI(muted) {
    if (!muteBtn) return;
    var show = state === "connected";
    muteBtn.hidden = !show;
    muteBtn.classList.toggle("hidden", !show);
    muteBtn.setAttribute(
      "aria-label",
      muted ? "Unmute microphone" : "Mute microphone"
    );
    muteBtn.classList.toggle("voice-muted", !!muted);
    var icon = muteBtn.querySelector(".iconify");
    if (icon) {
      var cls = (icon.className || "").replace(/lucide--[a-z0-9-]+/g, "").trim();
      icon.className = ("iconify " + (muted ? "lucide--mic-off" : "lucide--mic") + " " + cls).trim();
    }
  }

  // Reset buttons/mute/audioBlocked. Keeps any sticky error text so failures
  // are not silently overwritten by the "idle" label.
  function resetUI(force) {
    if (state !== "idle" && !force) return;
    setStatus(errorMsg || STATUS_IDLE);
    setCallMode(true);
    setMuteUI(false);
    audioBlocked = false;
  }

  /* ---------- mic level meter (local voice activity) ---------- */

  function startLevelMeter() {
    if (!micTrack || !micTrack.mediaStreamTrack || !levelEl) return;
    stopLevelMeter();
    var AC = window.AudioContext || window.webkitAudioContext;
    if (!AC) return;
    try {
      audioCtx = new AC();
      var src = audioCtx.createMediaStreamSource(new MediaStream([micTrack.mediaStreamTrack]));
      analyser = audioCtx.createAnalyser();
      analyser.fftSize = 512;
      src.connect(analyser);
      levelBuf = new Uint8Array(analyser.fftSize);
      loopLevel();
    } catch (e) {
      // best-effort: meter is cosmetic; the call itself is unaffected
      stopLevelMeter();
    }
  }

  function loopLevel() {
    if (!analyser) return;
    analyser.getByteTimeDomainData(levelBuf);
    var sum = 0;
    for (var i = 0; i < levelBuf.length; i++) {
      var v = (levelBuf[i] - 128) / 128;
      sum += v * v;
    }
    var rms = Math.sqrt(sum / levelBuf.length);
    var speaking = rms > 0.04;
    if (levelEl) {
      levelEl.style.setProperty("--level", String(Math.min(1, rms * 6)));
      levelEl.classList.toggle("voice-speaking", speaking);
      if (speaking) levelEl.setAttribute("data-speaking", "true");
      else levelEl.removeAttribute("data-speaking");
    }
    levelRaf = requestAnimationFrame(loopLevel);
  }

  function stopLevelMeter() {
    if (levelRaf) { cancelAnimationFrame(levelRaf); levelRaf = null; }
    if (audioCtx) { try { audioCtx.close(); } catch (e) {} audioCtx = null; }
    analyser = null;
    levelBuf = null;
    if (levelEl) {
      levelEl.style.setProperty("--level", "0");
      levelEl.classList.remove("voice-speaking");
      levelEl.removeAttribute("data-speaking");
    }
  }

  /* ---------- live transcription (lk.transcription text stream) ---------- */

  function voiceAddLine(isUser) {
    var wrap = document.createElement("div");
    wrap.className = isUser ? "chat chat-end memory-rise" : "chat chat-start memory-rise";
    var bubble = document.createElement("div");
    bubble.className = isUser
      ? "chat-bubble chat-bubble-primary max-w-[85%]"
      : "chat-bubble chat-bubble-neutral max-w-[85%]";
    var p = document.createElement("p");
    p.className = "whitespace-pre-wrap break-words";
    p.style.opacity = "0.7"; // interim; committed turns go full opacity
    bubble.appendChild(p);
    wrap.appendChild(bubble);
    var log = document.getElementById("chat-messages");
    if (log) {
      log.appendChild(wrap);
      var scroller = document.getElementById("chat-log");
      if (scroller) scroller.scrollTop = scroller.scrollHeight;
    }
    return p;
  }

  function voiceCommit(p) {
    if (p) p.style.opacity = "1";
  }

  function voiceRegisterTranscription(r) {
    // Sender identity distinguishes user (our own identity) from agent replies.
    r.registerTextStreamHandler("lk.transcription", async function (reader, participantInfo) {
      var identity = participantInfo && participantInfo.identity;
      var isUser = identity === r.localParticipant.identity;
      var el = isUser ? currentUserLine : currentAgentLine;
      if (!el) {
        el = voiceAddLine(isUser);
        if (isUser) currentUserLine = el; else currentAgentLine = el;
      }
      try {
        for await (var chunk of reader) {
          if (isUser) {
            el.textContent = chunk;      // user speech is cumulative → replace
          } else {
            el.textContent += chunk;     // agent reply streams as deltas → append
          }
        }
        var attrs = (reader.info && reader.info.attributes) || {};
        if (attrs["lk.transcription_final"] === "true") {
          voiceCommit(el);
          if (isUser) currentUserLine = null; else currentAgentLine = null;
        }
      } catch (e) {
        // abnormal close: drop the half-finished interim line
        if (isUser && currentUserLine === el) currentUserLine = null;
        if (!isUser && currentAgentLine === el) currentAgentLine = null;
      }
    });
  }

  /* ---------- click handlers (user gesture context) ---------- */

  function onCallClick() {
    if (state === "connecting") return; // ignore double-connect
    if (state === "connected") {
      // If playback is blocked, the button first enables audio instead of
      // hanging up; a second click then hangs up.
      if (audioBlocked && room) {
        enableAudio();
        return;
      }
      hangup();
      return;
    }
    startCall();
  }

  function onStatusClick() {
    if (state === "connected" && audioBlocked && room) enableAudio();
  }

  function enableAudio() {
    if (!room) return;
    room.startAudio().catch(function () {});
    // Optimistically clear; AudioPlaybackStatusChanged will re-flag if the
    // browser still withholds playback.
    audioBlocked = false;
    setStatus("connected");
  }

  function toggleMute() {
    if (state !== "connected" || !micTrack) return;
    if (micTrack.isMuted) {
      micTrack.unmute();
    } else {
      micTrack.mute();
    }
    setMuteUI(micTrack.isMuted);
  }

  /* ---------- start / hangup ---------- */

  async function startCall() {
    if (state !== "idle") return; // guard against double-connect
    errorMsg = null;              // clear any previous sticky error
    currentUserLine = null;
    currentAgentLine = null;
    if (!window.LivekitClient) {
      setStatus("Voice unavailable: LiveKit SDK not loaded");
      return;
    }
    // getUserMedia only exists in secure contexts (HTTPS or localhost). Check
    // early so we fail with a clear message instead of a silent connect/drop.
    if (!window.isSecureContext || !navigator.mediaDevices || !navigator.mediaDevices.getUserMedia) {
      setStatus("Voice needs HTTPS or localhost — mic blocked on this page");
      return;
    }

    var agent = currentAgentName();
    var identity = randomIdentity();
    state = "connecting";
    setStatus("connecting…");
    setCallMode(false); // show hangup affordance while connecting

    var token;
    try {
      token = await fetchToken(identity, agent);
    } catch (err) {
      failToIdle(err.message);
      return;
    }

    var Room = window.LivekitClient.Room;
    var RoomEvent = window.LivekitClient.RoomEvent;
    var Track = window.LivekitClient.Track;
    var createLocalAudioTrack = window.LivekitClient.createLocalAudioTrack;

    // fresh room + handlers registered BEFORE connect so no event is missed
    var r = new Room({ adaptiveStream: true });
    room = r;

    r.on(RoomEvent.TrackSubscribed, function (track) {
      if (track.kind === Track.Kind.Audio && audioEl) {
        track.attach(audioEl);
        var p = audioEl.play();
        if (p && p.catch) p.catch(function () {});
      }
    });
    r.on(RoomEvent.TrackUnsubscribed, function (track) {
      track.detach();
    });
    r.on(RoomEvent.AudioPlaybackStatusChanged, function () {
      if (!room) return;
      if (room.canPlaybackAudio) {
        audioBlocked = false;
        setStatus("connected");
      } else {
        audioBlocked = true;
        setStatus("Audio blocked — tap here or the call button to enable");
      }
    });
    r.on(RoomEvent.ActiveSpeakersChanged, function (speakers) {
      if (state !== "connected") return;
      var list = Array.isArray(speakers) ? speakers : (room && room.activeSpeakers) || [];
      var agentSpeaking = false;
      for (var i = 0; i < list.length; i++) {
        if (list[i] && !list[i].isLocal) { agentSpeaking = true; break; }
      }
      setStatus(agentSpeaking ? "Memory is speaking…" : "connected");
    });
    r.on(RoomEvent.Disconnected, function () {
      cleanup(true);
    });

    voiceRegisterTranscription(r);

    try {
      await r.connect(token.server_url, token.participant_token);
    } catch (err) {
      room = null;
      failToIdle("Voice connect failed: " + conciseError(err));
      return;
    }

    var track;
    try {
      track = await createLocalAudioTrack({}); // EC/NS/AGC all default on
    } catch (err) {
      try { r.disconnect(); } catch (e) {}
      room = null;
      failToIdle("Microphone unavailable: " + conciseError(err));
      return;
    }
    micTrack = track;

    try {
      await r.localParticipant.publishTrack(track);
    } catch (err) {
      try { track.stop(); } catch (e) {}
      try { r.disconnect(); } catch (e) {}
      micTrack = null;
      room = null;
      failToIdle("Voice publish failed: " + conciseError(err));
      return;
    }

    state = "connected";
    audioBlocked = false;
    setStatus("connected");
    setCallMode(false);
    setMuteUI(false);
    startLevelMeter();
  }

  function currentConversationId() {
    var m = location.search.match(/[?&]c=([^&]+)/);
    return m ? decodeURIComponent(m[1]) : "";
  }

  function refreshSessionRail() {
    if (window.MemoryChat && window.MemoryChat.refreshSessionRail) {
      window.MemoryChat.refreshSessionRail();
    }
  }

  async function fetchToken(identity, agent) {
    var res;
    try {
      // Same headers as chat.js's /api/chat fetch: Content-Type only.
      var body = { identity: identity, agent: agent, client: "web" };
      var cid = currentConversationId();
      if (cid) body.conversation_id = cid;
      res = await fetch("/api/token", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify(body),
      });
    } catch (err) {
      throw new Error("Voice: could not reach the gateway");
    }

    if (!res.ok) {
      var msg = "Voice token error " + res.status;
      try {
        var j = await res.json();
        if (j && j.error) msg = j.error;
      } catch (e) {}
      throw new Error(msg);
    }

    var data = await res.json();
    if (!data || !data.server_url || !data.participant_token) {
      throw new Error("Voice: invalid token response");
    }
    return data;
  }

  function hangup() {
    if (state === "idle") return;
    var r = room, t = micTrack;
    if (r && r.localParticipant && t) {
      try { r.localParticipant.unpublishTrack(t); } catch (e) {}
    }
    if (t) {
      try { t.stop(); } catch (e) {}
    }
    if (r) {
      try { r.disconnect(); } catch (e) {}
    }
    cleanup(false); // Disconnected handler also runs; guarded double-reset
  }

  // Shared teardown: drop refs, reset state + UI. `fromEvent` marks the
  // Disconnected-room path (room already gone; disconnect() not re-called).
  function cleanup(fromEvent) {
    if (micTrack && !fromEvent) {
      try { micTrack.stop(); } catch (e) {}
    }
    stopLevelMeter();
    currentUserLine = null;
    currentAgentLine = null;
    room = null;
    micTrack = null;
    audioBlocked = false;
    state = "idle";
    resetUI(true);
    refreshSessionRail();
  }

  // Reset to idle while preserving an error message (so failures are visible,
  // not silently overwritten by the idle label). `msg` omitted → plain idle.
  function failToIdle(msg) {
    stopLevelMeter();
    state = "idle";
    audioBlocked = false;
    errorMsg = msg || null;
    setCallMode(true);
    setMuteUI(false);
    setStatus(msg || STATUS_IDLE);
  }

  function conciseError(err) {
    if (!err) return "unknown error";
    if (err.message) return err.message;
    return String(err);
  }

  /* ---------- bootstrap ---------- */

  if (document.readyState === "loading") {
    document.addEventListener("DOMContentLoaded", init);
  } else {
    init();
  }

  // Minimal test/debug surface; the UI is driven entirely by element wiring.
  window.MemoryVoice = {
    start: startCall,
    hangup: hangup,
    toggleMute: toggleMute,
    get state() { return state; },
    get muted() { return !!(micTrack && micTrack.isMuted); },
  };
})();
