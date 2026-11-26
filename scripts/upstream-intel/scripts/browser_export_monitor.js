// 在「已登录」的 sub2api 面板页面控制台运行，导出渠道监控数据（成功率 + 延迟）。
//
// 适用：面板开启了渠道监控（channel_monitor_enabled），但 model_plaza 关闭、
// 或不想把面板 token 交出去的情况。
//
// 为什么要在你的浏览器里跑：
//   面板的 access token 带会话绑定（JWT 里的 bnd），IP/UA 一变就会触发
//   SESSION_BINDING_MISMATCH，服务端会吊销整个会话家族——等于把你踢下线。
//   在你自己浏览器里跑，绑定天然匹配，零风险。
//
// 用法：
//   1. 打开已登录的面板任意页面
//   2. F12 → Console → 粘贴本文件 → 回车
//   3. 下载 upstream-monitor-<host>.json

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

  const list = await get('/api/v1/channel-monitors');
  const items = (list?.data?.items) || [];
  if (!items.length) {
    throw new Error('没有监控项：面板可能未开启渠道监控（channel_monitor_enabled）');
  }

  // 逐个取详情：列表只给主模型，详情才有全部模型的 7/15/30 天可用率
  const detailById = new Map();
  for (const item of items) {
    try {
      const detail = await get(`/api/v1/channel-monitors/${item.id}/status`);
      detailById.set(item.id, detail?.data || null);
    } catch (err) {
      console.warn(`monitor ${item.id} detail failed:`, err.message);
    }
  }

  const channels = items.map((item) => {
    const detail = detailById.get(item.id) || {};
    const models = (detail.models || []).map((m) => ({
      model: m.model,
      latest_status: m.latest_status,
      latest_latency_ms: m.latest_latency_ms,
      avg_latency_7d_ms: m.avg_latency_7d_ms,
      availability_7d: m.availability_7d,
      availability_15d: m.availability_15d,
      availability_30d: m.availability_30d,
    }));
    return {
      monitor_id: item.id,
      name: item.name,
      provider: item.provider,
      group_name: item.group_name,
      primary_model: item.primary_model,
      primary_status: item.primary_status,
      primary_latency_ms: item.primary_latency_ms,
      availability_7d: item.availability_7d,
      models,
      timeline: item.timeline || [],
    };
  });

  const payload = {
    schema: 'upstream-monitor/v1',
    source: base,
    generated_at: new Date().toISOString(),
    channel_count: channels.length,
    channels,
  };

  const blob = new Blob([JSON.stringify(payload, null, 2)], { type: 'application/json' });
  const link = document.createElement('a');
  link.href = URL.createObjectURL(blob);
  link.download = `upstream-monitor-${location.hostname}.json`;
  document.body.appendChild(link);
  link.click();
  link.remove();

  console.log(`导出 ${channels.length} 个监控渠道`);
  return payload;
})();
