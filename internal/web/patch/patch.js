// ApiYard 前端补丁：由服务端注入到 index.html 的 <head> 最前面，在 YApi 预构建前端之前执行。
// 预构建前端固定为 yapi-vendor@1.12.0，下面按 DOM 结构和文字匹配的规则都以这个版本为准。
(function () {
  'use strict';

  // 1. 阻止 Google Analytics 统计。
  // 原版通过 document.createElement('img').src = 'https://www.google-analytics.com/collect?...'
  // 发送请求，参数里带着服务器主机名，内网部署也会被发到外部。
  var srcDesc = Object.getOwnPropertyDescriptor(HTMLImageElement.prototype, 'src');
  var blockedImage = /^https?:\/\/(www\.)?google-analytics\.com\//i;
  Object.defineProperty(HTMLImageElement.prototype, 'src', {
    configurable: true,
    enumerable: srcDesc.enumerable,
    get: srcDesc.get,
    set: function (value) {
      if (blockedImage.test(String(value))) {
        return;
      }
      srcDesc.set.call(this, value);
    }
  });

  // 2. 隐藏 v0.1 暂不支持的功能入口。
  // match：要找的元素；text：元素文字需完全相同（可选）；hide：实际隐藏的祖先元素（默认隐藏元素本身）。
  var HIDDEN_ENTRIES = [
    { match: '.ant-tabs-tab', text: '测试集合' },            // 自动化测试
    { match: '.ant-tabs-tab', text: '高级Mock' },
    { match: '.ant-tabs-tab', text: 'Swagger自动同步' },
    { match: '.ant-tabs-tab', text: '全局mock脚本' },        // ApiYard 不执行脚本（见 architecture.md D5）
    { match: 'a[href$="/wiki"]', hide: 'li' },               // 项目导航里的 Wiki
    { match: 'a[href="/statistic"]', hide: 'li' }            // 右上角菜单里的“系统信息”
  ];

  function hideEntries() {
    HIDDEN_ENTRIES.forEach(function (rule) {
      var nodes = document.querySelectorAll(rule.match);
      for (var i = 0; i < nodes.length; i++) {
        var node = nodes[i];
        if (rule.text && node.textContent.trim() !== rule.text) {
          continue;
        }
        var target = rule.hide ? node.closest(rule.hide) : node;
        if (target && target.style.display !== 'none') {
          target.style.display = 'none';
        }
      }
    });
  }

  // 前端是单页应用，页面切换时 DOM 会重建，所以持续监听变化；同一帧内的多次变化只处理一次
  var scheduled = false;
  function scheduleHide() {
    if (scheduled) {
      return;
    }
    scheduled = true;
    window.requestAnimationFrame(function () {
      scheduled = false;
      hideEntries();
    });
  }

  document.addEventListener('DOMContentLoaded', function () {
    hideEntries();
    new MutationObserver(scheduleHide).observe(document.body, { childList: true, subtree: true });
  });
})();
