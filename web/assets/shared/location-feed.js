/* A single writer for a rider's location. Sources know nothing about maps or APIs. */
window.CityFlowLocationFeed = ({ send, onPosition, onSource, interval = 3200 }) => {
  let current, queue = Promise.resolve(), lastFinished = -Infinity;
  const wait = (ms, signal) => new Promise((resolve, reject) => {
    const abort = () => { clearTimeout(timer); reject(new DOMException('Source stopped', 'AbortError')); };
    const timer = setTimeout(() => { signal.removeEventListener('abort', abort); resolve(); }, ms);
    signal.addEventListener('abort', abort, {once:true});
    if (signal.aborted) abort();
  });
  return {
    acquire(name) {
      current?.abort(); const controller = new AbortController(); current = controller; onSource?.(name);
      return {
        publish(point) {
          const operation = queue.catch(() => {}).then(async () => {
            if (controller.signal.aborted) throw new DOMException('Source stopped','AbortError');
            await wait(Math.max(0, interval - (performance.now() - lastFinished)),controller.signal);
            let position;
            try { position = await send(point, controller.signal, name); }
            finally { lastFinished = performance.now(); }
            if (controller.signal.aborted) throw new DOMException('Source stopped','AbortError');
            onPosition(position,name); return position;
          });
          queue = operation; return operation;
        },
        release() { controller.abort(); if (current === controller) { current = null; onSource?.(null); } },
      };
    },
    destroy() { current?.abort(); current = null; },
  };
};
