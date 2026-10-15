// Prism 只在调用方传入单个代码元素时运行，不注册自动页面高亮或 Worker 消息监听
window.Prism = { manual: true, disableWorkerMessageHandler: true };

const prism = require('prismjs/components/prism-core');
require('prismjs/components/prism-markup');
require('prismjs/components/prism-css');
require('prismjs/components/prism-clike');
require('prismjs/components/prism-javascript');
require('prismjs/components/prism-typescript');
require('prismjs/components/prism-go');
require('prismjs/components/prism-json');
require('prismjs/components/prism-bash');
require('prismjs/components/prism-python');
require('prismjs/components/prism-sql');
require('prismjs/components/prism-yaml');
require('prismjs/components/prism-markdown');

const languages = new Map([
  ['markup', 'markup'], ['html', 'markup'], ['xml', 'markup'],
  ['svg', 'markup'], ['mathml', 'markup'], ['css', 'css'],
  ['clike', 'clike'], ['javascript', 'javascript'], ['js', 'javascript'],
  ['typescript', 'typescript'], ['ts', 'typescript'], ['go', 'go'],
  ['golang', 'go'], ['json', 'json'], ['webmanifest', 'json'],
  ['bash', 'bash'], ['shell', 'bash'], ['sh', 'bash'],
  ['python', 'python'], ['py', 'python'], ['sql', 'sql'],
  ['yaml', 'yaml'], ['yml', 'yaml'], ['markdown', 'markdown'], ['md', 'markdown'],
]);

window.ShironeHighlight = function highlightCode(element) {
  if (!element || element.nodeName !== 'CODE' || typeof element.textContent !== 'string') return false;
  const language = languages.get(prism.util.getLanguage(element).toLowerCase());
  if (!language) return false;
  const source = element.textContent;
  // 写入内容仅来自 Prism 对纯文本的转义与标记，原始代码不作为 HTML 解析
  element.innerHTML = prism.highlight(source, prism.languages[language], language);
  return true;
};
