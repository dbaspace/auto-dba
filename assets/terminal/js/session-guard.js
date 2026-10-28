/**
 * 识别终端「命令执行中」，关页前提示；切到后台改标题。
 * 须在 gotty.js 之前加载，以便包装 WebSocket。
 * 浏览器无法禁止切换标签，保活依赖服务端 Ping。
 * 终端 /terminal/ws 首次未进入 OPEN 即失败后，同一地址不再发起连接。
 */
(function () {
  var busy = false;
  var baseTitle = document.title || "Terminal";
  var busyPrefix = "[命令执行中] ";
  // 常见 shell 提示符行尾
  var promptRe = /(?:^|[\r\n])[^\r\n]*([#$%]|]\s?\$|]\s?#)\s*$/;
  var deadKeys = Object.create(null);
  var tipShown = false;

  function setBusy(next) {
    busy = !!next;
    if (document.hidden && busy) {
      document.title = busyPrefix + baseTitle;
    } else if (!document.hidden) {
      document.title = baseTitle;
    }
  }

  function onBeforeUnload(e) {
    if (!busy) return;
    e.preventDefault();
    e.returnValue = "";
    return "";
  }

  window.addEventListener("beforeunload", onBeforeUnload);
  document.addEventListener("visibilitychange", function () {
    if (document.hidden && busy) {
      document.title = busyPrefix + baseTitle;
    } else {
      document.title = baseTitle;
    }
  });

  function isTerminalWS(url) {
    var s = String(url || "");
    return s.indexOf("/terminal/ws") >= 0;
  }

  function deadKey(url) {
    return String(url || "");
  }

  function showSessionDeadTip() {
    if (tipShown) return;
    tipShown = true;
    var msg = "终端会话已失效，请重新从平台打开";
    try {
      document.title = msg;
      var el = document.createElement("div");
      el.setAttribute("role", "alert");
      el.style.cssText =
        "position:fixed;z-index:99999;left:0;right:0;top:0;padding:12px 16px;" +
        "background:#b91c1c;color:#fff;font:14px/1.4 system-ui,sans-serif;text-align:center";
      el.textContent = msg;
      document.documentElement.appendChild(el);
    } catch (err) {
      /* ignore */
    }
  }

  function fakeClosedSocket() {
    var listeners = { open: [], close: [], error: [], message: [] };
    var ws = {
      readyState: 3, // CLOSED
      bufferedAmount: 0,
      extensions: "",
      protocol: "",
      url: "",
      binaryType: "blob",
      onopen: null,
      onclose: null,
      onerror: null,
      onmessage: null,
      send: function () {},
      close: function () {},
      addEventListener: function (type, fn) {
        if (listeners[type]) listeners[type].push(fn);
      },
      removeEventListener: function (type, fn) {
        var arr = listeners[type];
        if (!arr) return;
        var i = arr.indexOf(fn);
        if (i >= 0) arr.splice(i, 1);
      },
      dispatchEvent: function () {
        return true;
      },
    };
    setTimeout(function () {
      var errEv = { type: "error", target: ws };
      if (typeof ws.onerror === "function") ws.onerror(errEv);
      listeners.error.forEach(function (fn) {
        try {
          fn(errEv);
        } catch (e) {}
      });
      var closeEv = { type: "close", code: 4001, reason: "session dead", wasClean: true, target: ws };
      if (typeof ws.onclose === "function") ws.onclose(closeEv);
      listeners.close.forEach(function (fn) {
        try {
          fn(closeEv);
        } catch (e) {}
      });
    }, 0);
    return ws;
  }

  var OrigWS = window.WebSocket;
  if (!OrigWS) return;

  function wrap(url, protocols) {
    var key = deadKey(url);
    if (isTerminalWS(url) && deadKeys[key]) {
      showSessionDeadTip();
      return fakeClosedSocket();
    }

    var ws = protocols === undefined ? new OrigWS(url) : new OrigWS(url, protocols);
    var opened = false;
    var terminal = isTerminalWS(url);

    if (terminal) {
      ws.addEventListener("open", function () {
        opened = true;
      });
      function markDead() {
        if (opened) return;
        deadKeys[key] = true;
        showSessionDeadTip();
      }
      ws.addEventListener("error", markDead);
      ws.addEventListener("close", markDead);
    }

    var send = ws.send;
    ws.send = function (data) {
      if (typeof data === "string" && data.length > 1 && data.charAt(0) === "1") {
        try {
          var decoded = atob(data.slice(1));
          if (decoded.indexOf("\r") >= 0 || decoded.indexOf("\n") >= 0) {
            setBusy(true);
          }
        } catch (err) {
          /* ignore */
        }
      }
      return send.call(ws, data);
    };
    ws.addEventListener("message", function (ev) {
      var data = ev.data;
      if (typeof data !== "string" || data.length < 2 || data.charAt(0) !== "1") return;
      try {
        var text = atob(data.slice(1));
        if (promptRe.test(text)) {
          setBusy(false);
        }
      } catch (err) {
        /* ignore */
      }
    });
    ws.addEventListener("close", function () {
      setBusy(false);
    });
    return ws;
  }

  wrap.prototype = OrigWS.prototype;
  wrap.CONNECTING = OrigWS.CONNECTING;
  wrap.OPEN = OrigWS.OPEN;
  wrap.CLOSING = OrigWS.CLOSING;
  wrap.CLOSED = OrigWS.CLOSED;
  window.WebSocket = wrap;
})();
