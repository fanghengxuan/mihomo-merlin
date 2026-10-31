<!DOCTYPE html>
<html lang="zh-CN">
<head>
<meta charset="utf-8">
<script>
/* 软件中心离线安装时通过这些变量识别插件与平台，格式与 fancyss 保持一致 */
var PKG_NAME = "mihomo";
var PKG_ARCH = "hnd";     /* axhnd.675x / armv7l，内核 4.1 */
var PKG_TYPE = "full";
var PKG_EXTA = "";
var PKG_VERSION = "1.7.5";
</script>
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Mihomo 魔方</title>
<style>
  html,body{margin:0;height:100%;background:#0f1216;color:#e6edf3;
    font-family:-apple-system,BlinkMacSystemFont,"PingFang SC","Microsoft YaHei",sans-serif}
  .wrap{display:flex;flex-direction:column;height:100%}
  .bar{display:flex;align-items:center;gap:10px;padding:9px 14px;background:#171b21;
    border-bottom:1px solid rgba(255,255,255,.08);font-size:13px;flex:0 0 auto}
  .dot{width:8px;height:8px;border-radius:50%;background:#8b98a9}
  .dot.on{background:#3ddc97;box-shadow:0 0 0 3px rgba(61,220,151,.18)}
  .btn{margin-left:auto;padding:5px 12px;border-radius:9px;border:1px solid rgba(255,255,255,.18);
    background:#1e242c;color:#e6edf3;font-size:12px;text-decoration:none}
  .btn:hover{background:#252d37}
  iframe{flex:1;width:100%;border:0;background:#0f1216}
  #tip{padding:10px 14px;font-size:12.5px;color:#8b98a9}
</style>
</head>
<body>
<div class="wrap">
  <div class="bar">
    <span class="dot" id="dot"></span>
    <img src="/res/icon-mihomo.png" alt="" width="20" height="20" style="border-radius:6px;vertical-align:-4px"><strong>Mihomo 魔方</strong>
    <span id="addr" style="color:#8b98a9"></span>
    <a class="btn" id="open" target="_blank" rel="noopener">新窗口打开 ↗</a>
  </div>
  <div id="tip">管理面板运行在本机 9380 端口；如果下方长时间空白，请检查「面板是否启动」。</div>
  <iframe id="fr" src="about:blank" title="mihomo 面板"></iframe>
</div>
<script>
  // 面板与软件中心同主机不同端口，这里用当前访问的主机名拼出面板地址
  var host = location.hostname || '192.168.50.1';
  var url = 'http://' + host + ':9380/';
  document.getElementById('fr').src = url;
  document.getElementById('open').href = url;
  document.getElementById('addr').textContent = url;
  document.getElementById('tip').style.display = 'none';

  // 用图片探活的方式粗略判断面板是否起来（跨端口无法 fetch，避免 CORS 报错）
  var img = new Image();
  img.onload = img.onerror = function () {
    document.getElementById('dot').className = 'dot on';
  };
  img.src = url + 'favicon.ico?_=' + Date.now();
</script>
</body>
</html>
