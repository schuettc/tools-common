// A stand-in for a tool's mermaid bundle, in the shape esbuild's
// --format=iife --global-name=fakeMermaid gives it: parse rejects a diagram
// with a ";" in it, with a reason longer than two lines, throws on "throw",
// and leaves its promise pending on "hang".
var fakeMermaid = (() => {
  const parse = (text) => {
    if (text.includes('throw')) throw new Error('thrown at once\nline two\nline three');
    if (text.includes('hang')) return new Promise(() => {});
    if (text.includes(';')) {
      return Promise.reject(new Error('Parse error on line 9:\n...D->>D: owner = new session; unan\n----------------------^\nExpecting a message'));
    }
    return Promise.resolve({ diagramType: 'sequence' });
  };
  return { default: { parse } };
})();
