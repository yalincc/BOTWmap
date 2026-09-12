/* ============ 旷野之息互动地图 · 入口 ============ */
'use strict';

(function () {
  const canvas = document.getElementById('mapCanvas');
  const map = new BotwMap(canvas, BOTW_DATA);

  UI.init(map);        // 先绑定 UI（buildLayerList 等）
  UI.loadState();      // 再恢复本地状态（完成/自定义/图层）
  UI.applyHash();      // URL 分享状态优先于本地图层状态

  UI.renderStats();
  UI.updateLayerList();

  // 视图变化时更新分享链接（轻量，不刷新 hash，避免抖动）
  map._onViewChange = () => {
    // 不做即时写回，用户点“分享”才生成链接
  };

  // 窗口尺寸变化
  let resizeTimer = null;
  window.addEventListener('resize', () => {
    clearTimeout(resizeTimer);
    resizeTimer = setTimeout(() => map._resize(), 120);
  });

  // 手机版侧栏抽屉：☰ 打开，× / 遮罩 / Esc 收起
  const sidebar = document.getElementById('sidebar');
  const sideMask = document.getElementById('sideMask');
  function setSidebar(open) {
    sidebar.classList.toggle('open', open);
    sideMask.classList.toggle('hidden', !open);
  }
  document.getElementById('btnSidebar').addEventListener('click', () => setSidebar(!sidebar.classList.contains('open')));
  document.getElementById('sideClose').addEventListener('click', () => setSidebar(false));
  sideMask.addEventListener('click', () => setSidebar(false));
  window.addEventListener('keydown', e => {
    if (e.key === 'Escape' && sidebar.classList.contains('open')) setSidebar(false);
  });
  // 桌面端（>760px）侧栏始终可见，收起态不生效
  window.addEventListener('resize', () => {
    if (window.innerWidth > 760) setSidebar(false);
  });

  // 提示
  setTimeout(() => {
    UI.toast('点击标注查看详情 · 勾选图层可多选同显 · 完成度自动保存');
  }, 600);

  window.__botwMap = map; // 便于调试
})();
