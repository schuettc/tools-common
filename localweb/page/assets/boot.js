// Classic script for <head>: applies the remembered theme before first paint
// (modules are deferred and would flash). Mirrors theme.js.
//   <script src="/_kit/boot.js" data-tool="cull"></script>
(function () {
  try {
    var tool = document.currentScript && document.currentScript.dataset.tool;
    var mode = tool && localStorage.getItem(tool + '.theme');
    if (mode === 'light' || mode === 'dark') document.documentElement.dataset.theme = mode;
  } catch (e) {
    // storage denied: follow the system
  }
})();
