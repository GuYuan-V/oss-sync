// theme.js 在页面首帧绘制前执行，避免主题闪烁。
// 控制台偏好键 oss-console-theme，公开博客使用 oss-blog-theme（由各自页面调用 initTheme）。
(function () {
  "use strict";

  function readPreference(key) {
    try {
      var raw = localStorage.getItem(key);
      if (raw === "light" || raw === "dark" || raw === "auto") return raw;
    } catch (e) {
      /* localStorage 不可用时退回 auto */
    }
    return "auto";
  }

  function effectiveTheme(pref) {
    if (pref === "light" || pref === "dark") return pref;
    return window.matchMedia && window.matchMedia("(prefers-color-scheme: dark)").matches
      ? "dark"
      : "light";
  }

  function applyTheme(key) {
    var pref = readPreference(key);
    document.documentElement.setAttribute("data-theme", effectiveTheme(pref));
    document.documentElement.setAttribute("data-theme-pref", pref);
  }

  window.OSSTheme = {
    readPreference: readPreference,
    effectiveTheme: effectiveTheme,
    applyTheme: applyTheme,
    setPreference: function (key, pref) {
      try {
        localStorage.setItem(key, pref);
      } catch (e) {
        /* 忽略存储失败 */
      }
      applyTheme(key);
    },
  };

  // 控制台页面默认使用控制台主题键；公开博客在页面末尾用 initTheme("oss-blog-theme") 覆盖。
  var key = document.documentElement.getAttribute("data-theme-key") || "oss-console-theme";
  applyTheme(key);
})();
