// ApiYard 登录页：替代 YApi 的首页和登录页，调用与原版相同的 /api/user/login、/api/user/reg。
(function () {
  'use strict';

  var $ = function (id) { return document.getElementById(id); };

  // 登录后回到原来要去的页面；只接受站内路径，防止被构造成跳转到外部网站的链接
  function target() {
    var r = new URLSearchParams(location.search).get('redirect') || '';
    if (r.charAt(0) !== '/' || r.charAt(1) === '/' || r.charAt(1) === '\\' || /^\/login(\/|\?|$)/.test(r) || r === '/') {
      return '/group';
    }
    return r;
  }

  function post(url, body) {
    return fetch(url, {
      method: 'POST',
      credentials: 'same-origin',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(body)
    }).then(function (res) { return res.json(); });
  }

  var tabs = { login: $('tab-login'), reg: $('tab-reg') };
  var forms = { login: $('form-login'), reg: $('form-reg') };
  var titles = {
    login: ['登录 ApiYard', '使用邮箱和密码登录'],
    reg: ['注册 ApiYard', '注册后会自动登录，并创建你的个人空间']
  };

  function select(name) {
    Object.keys(tabs).forEach(function (k) {
      tabs[k].setAttribute('aria-selected', String(k === name));
      forms[k].hidden = k !== name;
    });
    $('title').textContent = titles[name][0];
    $('subtitle').textContent = titles[name][1];
    var first = forms[name].querySelector('input');
    if (first) { first.focus(); }
  }

  tabs.login.addEventListener('click', function () { select('login'); });
  tabs.reg.addEventListener('click', function () { select('reg'); });

  function showError(form, msg) {
    var el = form.querySelector('.error');
    el.textContent = msg;
    el.hidden = !msg;
  }

  function submit(form, url, body) {
    var button = form.querySelector('button[type="submit"]');
    button.disabled = true;
    showError(form, '');
    post(url, body).then(function (res) {
      if (res.errcode === 0) {
        location.replace(target());
        return;
      }
      showError(form, res.errmsg || '操作失败');
      button.disabled = false;
    }).catch(function () {
      showError(form, '无法连接服务器，请稍后重试');
      button.disabled = false;
    });
  }

  forms.login.addEventListener('submit', function (e) {
    e.preventDefault();
    var email = $('login-email').value.trim();
    var password = $('login-password').value;
    if (!email || !password) {
      showError(forms.login, '请输入邮箱和密码');
      return;
    }
    submit(forms.login, '/api/user/login', { email: email, password: password });
  });

  forms.reg.addEventListener('submit', function (e) {
    e.preventDefault();
    var username = $('reg-username').value.trim();
    var email = $('reg-email').value.trim();
    var password = $('reg-password').value;
    if (!username || !email || !password) {
      showError(forms.reg, '请填写用户名、邮箱和密码');
      return;
    }
    if (password !== $('reg-confirm').value) {
      showError(forms.reg, '两次输入的密码不一致');
      return;
    }
    submit(forms.reg, '/api/user/reg', { username: username, email: email, password: password });
  });

  $('version').textContent = window.APIYARD_VERSION || '';

  // 已登录直接进入；关闭注册时隐藏注册入口
  fetch('/api/user/status', { credentials: 'same-origin' })
    .then(function (res) { return res.json(); })
    .then(function (res) {
      if (res.errcode === 0) {
        location.replace(target());
        return;
      }
      if (res.canRegister === false) {
        $('tabs').hidden = true;
        select('login');
      } else if (location.hash === '#register') {
        select('reg');
      }
    })
    .catch(function () { /* 状态查询失败时照常显示登录表单 */ });
})();
