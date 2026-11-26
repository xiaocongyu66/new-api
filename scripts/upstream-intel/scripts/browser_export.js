// 在「已登录」的上游面板页面控制台里运行，导出模型价格 + 成功率 JSON。
//
// 为什么这样设计：有些上游用人机验证（Cloudflare Turnstile）挡住自动化登录。
// 那道人机验证本来就是为了区分人和机器，绕过它既不可靠也不合适。
// 但验证只在「登录」这一步——登录之后，浏览器里已经有会话，取数完全可以
// 自动完成。所以把自动化放在登录之后：人只做一次验证，其余交给脚本。
//
// 用法：
//   1. 在浏览器里登录该上游面板（人机验证你自己点）
//   2. F12 → Console → 粘贴本文件全部内容 → 回车
//   3. 浏览器会下载 upstream-models.json
//
// 也可以在地址栏用 javascript: 前缀运行（部分浏览器会过滤，建议用 Console）。

(async () => {
  const base = location.origin;
  const get = async (url) => {
    const res = await fetch(url, {
      headers: { 'Content-Type': 'application/json' },
      credentials: 'include',
    });
    if (!res.ok) throw new Error(`${url} -> HTTP ${res.status}`);
    return res.json();
  };

  const pricing = await get('/api/pricing');
  const rows = Array.isArray(pricing?.data) ? pricing.data : [];
  if (!rows.length) throw new Error(' /api/pricing 没有返回模型列表');

  // 性能接口可能未开放；失败不影响价格导出。
  let summary = null;
  try {
    summary = await get('/api/perf-metrics/summary?hours=24');
  } catch (err) {
    console.warn('成功率接口不可用：', err.message);
  }
  const byModel = new Map(
    ((summary?.data?.models) || []).map((m) => [m.model_name, m]),
  );

  const models = rows.map((row) => {
    const perf = byModel.get(row.model_name);
    return {
      model: row.model_name,
      quota_type: row.quota_type ?? 0,
      model_ratio: row.model_ratio ?? null,
      completion_ratio: row.completion_ratio ?? null,
      model_price: row.model_price ?? null,
      enable_groups: row.enable_groups ?? null,
      success_rate: perf?.success_rate ?? null,
      avg_latency_ms: perf?.avg_latency_ms ?? null,
    };
  });

  const payload = {
    schema: 'upstream-model-catalog/v1',
    source: base,
    generated_at: new Date().toISOString(),
    window_hours: 24,
    model_count: models.length,
    models,
  };

  const blob = new Blob([JSON.stringify(payload, null, 2)], {
    type: 'application/json',
  });
  const link = document.createElement('a');
  link.href = URL.createObjectURL(blob);
  link.download = `upstream-models-${location.hostname}.json`;
  document.body.appendChild(link);
  link.click();
  link.remove();

  console.log(
    `导出 ${models.length} 个模型，其中 ${models.filter((m) => m.success_rate !== null).length} 个带成功率`,
  );
  return payload;
})();
