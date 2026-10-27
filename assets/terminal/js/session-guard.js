/**
 * 识别终端「命令执行中」，关页前提示；切到后台改标题。
 * 须在 gotty.js 之前加载，以便包装 WebSocket。
 * 浏览器无法禁止切换标签，保活依赖服务端 Ping。
 */
(function () {
  var busy = false;
  var baseTitle = document.title || "Terminal";
  var busyPrefix = "[命令执行中] ";
  // 常见 shell 提示符行尾
  var promptRe = /(?:^|[\r\n])[^\r\n]*([#$%]|]\s?\$|]\s?#)\s*$/;

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

  var OrigWS = window.WebSocket;
  if (!OrigWS) return;

  function wrap(url, protocols) {
    var ws = protocols === undefined ? new OrigWS(url) : new OrigWS(url, protocols);
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
