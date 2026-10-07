/** Merge bursts, but publish even if events never become quiet. */
export function batchTopics(publish: (topics: string[]) => void, delay = 180, maxWait = 500) {
  const pending = new Set<string>();
  let quiet: ReturnType<typeof setTimeout> | undefined;
  let deadline: ReturnType<typeof setTimeout> | undefined;
  function cancel() {
    clearTimeout(quiet); clearTimeout(deadline);
    quiet = deadline = undefined; pending.clear();
  }
  function flush() {
    const topics = [...pending]; cancel();
    if (topics.length) publish(topics);
  }
  return {
    add(topics: string[]) {
      topics.forEach(topic => pending.add(topic));
      clearTimeout(quiet); quiet = setTimeout(flush, delay);
      if (deadline === undefined) deadline = setTimeout(flush, maxWait);
    },
    cancel,
  };
}
