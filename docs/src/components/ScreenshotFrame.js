import React from 'react';

/**
 * ScreenshotFrame — a device frame (desktop / mobile) for a screenshot
 * placeholder or a present screenshot.
 *
 * Props:
 *   - file   : the image file name (used to look up presence in the manifest)
 *   - label  : a short German label shown under the frame
 *   - device : "desktop" | "mobile"
 *
 * Presence is resolved from the build-time manifest at docs/src/data/screenshots.json.
 */
export default function ScreenshotFrame({ file, label, device = 'desktop' }) {
  let manifest = [];
  try {
    manifest = require('../data/screenshots.json');
  } catch {
    manifest = [];
  }
  const entry = manifest.find((s) => s.file === file);
  const present = Boolean(entry && entry.present);
  const frame = device === 'mobile' ? 'gear-shot-frame-mobile' : 'gear-shot-frame-desktop';

  return (
    <figure className={`gear-shot ${frame}`}>
      {present ? (
        <img src={`/img/screenshots/${file}`} alt={label} loading="lazy" />
      ) : (
        <div className="gear-shot-placeholder" role="img" aria-label={`Screenshot fehlt: ${file}`}>
          <span>📷 Screenshot fehlt</span>
          <code>{file}</code>
        </div>
      )}
      <figcaption>{label}</figcaption>
    </figure>
  );
}