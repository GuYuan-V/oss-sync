// metrics.js 刷新概览和管理员数据页上的实时服务器指标。
(function () {
  "use strict";

  function formatBytes(bytes) {
    var units = ["B", "KB", "MB", "GB", "TB"];
    var value = Number(bytes);
    var index = 0;
    while (value >= 1024 && index < units.length - 1) {
      value /= 1024;
      index += 1;
    }
    return (index === 0 ? String(Math.round(value)) : value.toFixed(1)) + " " + units[index];
  }

  function initSystemMetrics() {
    var container = document.querySelector("[data-system-metrics]");
    if (!container) return;
    var url = container.getAttribute("data-system-metrics-url");
    if (!url) return;

    function setMetric(name, value) {
      var element = container.querySelector('[data-system-metric="' + name + '"]');
      if (element) element.textContent = value;
    }

    function refresh() {
      window.fetch(url, { credentials: "same-origin" })
        .then(function (response) {
          if (!response.ok) throw new Error("live metrics request failed");
          return response.json();
        })
        .then(function (metrics) {
          setMetric("cpu-model", metrics.cpu_model_name);
          setMetric("cpu-usage", Number(metrics.cpu_usage_percent).toFixed(1) + "%");
          setMetric("memory-usage", Number(metrics.memory_usage_percent).toFixed(1) + "%");
          setMetric("memory-capacity", formatBytes(metrics.memory_used_bytes) + " / " + formatBytes(metrics.memory_total_bytes));
          setMetric("disk-storage", formatBytes(metrics.disk_used_bytes) + " / " + formatBytes(metrics.disk_total_bytes));
          if (container.querySelector('[data-system-metric="vault-storage"]')) {
            var vaultStorage = formatBytes(metrics.vault_storage_used);
            if (metrics.vault_storage_quota) vaultStorage += " / " + formatBytes(metrics.vault_storage_quota);
            setMetric("vault-storage", vaultStorage);
          }
        })
        .catch(function () {});
    }

    refresh();
    window.setInterval(refresh, 5000);
  }

  if (document.readyState === "loading") {
    document.addEventListener("DOMContentLoaded", initSystemMetrics);
  } else {
    initSystemMetrics();
  }
})();
