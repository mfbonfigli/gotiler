// Keeps an events stream open while the page is open: the server stops
// shortly after the last page closes. This script does not wait for Cesium to
// load, so a reloading page reconnects before the server's countdown ends.
import { token } from './api.js';
import { setStatus } from './notices.js';

const events = new EventSource(`/api/events?token=${encodeURIComponent(token)}`);
events.onerror = () => {
  if (events.readyState !== EventSource.OPEN) {
    setStatus('Lost the connection to gotiler: the viewer has stopped.', { error: true });
  }
};
