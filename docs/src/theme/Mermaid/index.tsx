import React, {
  useState,
  useRef,
  useCallback,
  useEffect,
  useLayoutEffect,
  useMemo,
  type ReactNode,
} from 'react';
import ErrorBoundary from '@docusaurus/ErrorBoundary';
import {ErrorBoundaryErrorMessageFallback} from '@docusaurus/theme-common';
import {
  MermaidContainerClassName,
  useMermaidConfig,
  useMermaidRenderResult,
} from '@docusaurus/theme-mermaid/client';
import type {Props} from '@theme/Mermaid';
import type {MermaidConfig} from 'mermaid';

import styles from './styles.module.css';

type RenderResultSvg = {svg: string; bindFunctions?: (e: HTMLElement) => void};

function MermaidSvg({
  svg,
  bindFunctions,
  containerClassName,
}: {
  svg: string;
  bindFunctions?: (e: HTMLElement) => void;
  containerClassName?: string;
}): ReactNode {
  const ref = useRef<HTMLDivElement>(null);

  useEffect(() => {
    const div = ref.current!;
    bindFunctions?.(div);
  }, [bindFunctions, svg]);

  return (
    <div
      ref={ref}
      className={`${MermaidContainerClassName} ${styles.svgContainer} ${containerClassName ?? ''}`}
      // eslint-disable-next-line react/no-danger
      dangerouslySetInnerHTML={{__html: svg}}
    />
  );
}

const clamp = (value: number, min: number, max: number): number =>
  Math.min(Math.max(value, min), max);

const MIN_ZOOM = 0.5;
const MAX_ZOOM = 8;

function ExpandableDiagram({value}: {value: string}): ReactNode {
  const [expanded, setExpanded] = useState(false);
  const close = useCallback(() => setExpanded(false), []);
  const open = useCallback(() => setExpanded(true), []);

  useEffect(() => {
    if (!expanded) {
      return undefined;
    }
    const onKey = (event: KeyboardEvent) => {
      if (event.key === 'Escape') {
        setExpanded(false);
      }
    };
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  }, [expanded]);

  return (
    <>
      <div className={styles.wrapper}>
        <ExpandButton onClick={open} />
        <InlineDiagram value={value} onExpand={open} />
      </div>
      {expanded ? <Overlay value={value} onClose={close} /> : null}
    </>
  );
}

function ExpandButton({onClick}: {onClick: () => void}): ReactNode {
  return (
    <button
      type="button"
      className={styles.expandButton}
      aria-label="Expand diagram"
      title="Expand diagram"
      onClick={onClick}>
      <svg viewBox="0 0 24 24" width="18" height="18" aria-hidden="true">
        <path d="M15 3h6v6h-2V5h-4V3zM9 3v2H5v4H3V3h6zm6 18h6v-6h-2v4h-4v2zm-6 0v-2H5v-4H3v6h6z" />
      </svg>
    </button>
  );
}

function InlineDiagram({value, onExpand}: {value: string; onExpand: () => void}): ReactNode {
  const renderResult = useMermaidRenderResult({text: value});
  if (renderResult === null) {
    return null;
  }
  return (
    <div
      className={styles.inline}
      role="button"
      tabIndex={0}
      aria-label="Expand diagram"
      onClick={onExpand}
      onKeyDown={(event) => {
        if (event.key === 'Enter' || event.key === ' ') {
          event.preventDefault();
          onExpand();
        }
      }}>
      <MermaidSvg svg={renderResult.svg} bindFunctions={renderResult.bindFunctions} />
    </div>
  );
}

// Crisp zoom: re-render labels as vector <text> instead of foreignObject HTML,
// so scaling stays sharp. Only used in the overlay; inline keeps defaults.
const crispConfig = (base: MermaidConfig): MermaidConfig => ({
  ...base,
  htmlLabels: false,
  flowchart: {...base.flowchart, htmlLabels: false},
});

type Vec2 = {x: number; y: number};

function Overlay({value, onClose}: {value: string; onClose: () => void}): ReactNode {
  const baseConfig = useMermaidConfig();
  // Memoize so the config reference is stable across renders — otherwise
  // useMermaidRenderResult re-renders mermaid on every render (infinite loop).
  const crisp = useMemo(() => crispConfig(baseConfig), [baseConfig]);
  const renderResult = useMermaidRenderResult({
    text: value,
    config: crisp,
  });

  const viewportRef = useRef<HTMLDivElement>(null);
  const svgWrapRef = useRef<HTMLDivElement>(null);
  const [viewport, setViewport] = useState<Vec2 | null>(null);
  const vbRef = useRef<[number, number, number, number] | null>(null);

  const [transform, setTransform] = useState({z: 1, tx: 0, ty: 0});

  useLayoutEffect(() => {
    const el = viewportRef.current;
    if (!el) {
      return undefined;
    }
    const measure = () =>
      setViewport({x: el.clientWidth, y: el.clientHeight});
    measure();
    const ro = new ResizeObserver(measure);
    ro.observe(el);
    return () => ro.disconnect();
  }, []);

  useLayoutEffect(() => {
    const wrap = svgWrapRef.current;
    if (!wrap || vbRef.current) {
      return;
    }
    const svgEl = wrap.querySelector('svg');
    if (!svgEl) {
      return;
    }
    const vb = svgEl.getAttribute('viewBox');
    if (vb) {
      const nums = vb.trim().split(/[\s,]+/).map(Number);
      if (nums.length === 4 && nums.every(Number.isFinite)) {
        vbRef.current = [nums[0], nums[1], nums[2], nums[3]];
      }
    }
  }, [renderResult]);

  const drag = useRef<{pointerId: number; sx: number; sy: number; ox: number; oy: number} | null>(null);

  const onPointerDown = useCallback((event: React.PointerEvent<HTMLDivElement>) => {
    const target = event.target as HTMLElement | null;
    if (target?.closest('button, a, [data-nopan]')) {
      return;
    }
    event.currentTarget.setPointerCapture(event.pointerId);
    drag.current = {
      pointerId: event.pointerId,
      sx: event.clientX,
      sy: event.clientY,
      ox: transform.tx,
      oy: transform.ty,
    };
  }, [transform]);

  const onPointerMove = useCallback((event: React.PointerEvent<HTMLDivElement>) => {
    const d = drag.current;
    if (!d || d.pointerId !== event.pointerId) {
      return;
    }
    setTransform((prev) => ({...prev, tx: d.ox + (event.clientX - d.sx), ty: d.oy + (event.clientY - d.sy)}));
  }, []);

  const onPointerUp = useCallback((event: React.PointerEvent<HTMLDivElement>) => {
    if (drag.current?.pointerId === event.pointerId) {
      drag.current = null;
    }
  }, []);

  const onWheel = useCallback(
    (event: React.WheelEvent<HTMLDivElement>) => {
      if (!viewport || !vbRef.current) {
        return;
      }
      event.preventDefault();
      setTransform((prev) => ({
        ...prev,
        z: clamp(prev.z * Math.exp(-event.deltaY * 0.0016), MIN_ZOOM, MAX_ZOOM),
      }));
    },
    [viewport],
  );

  const reset = useCallback(
    () => setTransform({z: 1, tx: 0, ty: 0}),
    [],
  );

  // Compute fitted box size.
  let box: {w: number; h: number} | null = null;
  if (viewport && vbRef.current) {
    const [, , vw, vh] = vbRef.current;
    const base = Math.min(viewport.x / vw, viewport.y / vh);
    const displayBase = base * transform.z;
    box = {w: vw * displayBase, h: vh * displayBase};
  }

  const touchActionNone = {touchAction: 'none' as const};

  return (
    <div
      className={styles.backdrop}
      data-testid="mermaid-overlay"
      ref={viewportRef}
      onWheel={onWheel}
      onPointerDown={onPointerDown}
      onPointerMove={onPointerMove}
      onPointerUp={onPointerUp}
      onPointerCancel={onPointerUp}>
      <button
        type="button"
        className={styles.closeButton}
        aria-label="Close and exit expanded mode"
        onClick={onClose}>
        <svg viewBox="0 0 24 24" width="20" height="20" aria-hidden="true">
          <path d="M18.3 5.7 12 12l6.3 6.3-1.4 1.4L10.6 13.4 4.3 19.7 2.9 18.3 9.2 12 2.9 5.7 4.3 4.3 10.6 10.6 16.9 4.3z" />
        </svg>
      </button>
      <div className={styles.toolbar}>
        <button
          type="button"
          className={styles.toolButton}
          aria-label="Zoom in"
          onClick={() => setTransform((p) => ({...p, z: clamp(p.z * 1.25, MIN_ZOOM, MAX_ZOOM)}))}>
          +
        </button>
        <button
          type="button"
          className={styles.toolButton}
          aria-label="Zoom out"
          onClick={() => setTransform((p) => ({...p, z: clamp(p.z / 1.25, MIN_ZOOM, MAX_ZOOM)}))}>
          −
        </button>
        <button type="button" className={styles.toolButton} onClick={reset}>
          Reset
        </button>
      </div>
      <div className={styles.hint}>Scroll to zoom · drag to pan</div>
      {renderResult === null ? (
        <div className={styles.loading} style={touchActionNone}>
          Rendering…
        </div>
      ) : (
        <div className={styles.stage} style={touchActionNone}>
          <div
            ref={svgWrapRef}
            className={styles.transform}
            data-testid="mermaid-overlay-transform"
            style={
              box
                ? {
                    width: box.w,
                    height: box.h,
                    transform: `translate(-50%, -50%) translate(${transform.tx}px, ${transform.ty}px)`,
                  }
                : undefined
            }>
            <MermaidSvg svg={renderResult.svg} bindFunctions={renderResult.bindFunctions} />
          </div>
        </div>
      )}
    </div>
  );
}

function MermaidRenderer({value}: Props): ReactNode {
  return <ExpandableDiagram value={value} />;
}

export default function Mermaid(props: Props): ReactNode {
  return (
    <ErrorBoundary
      fallback={(params) => <ErrorBoundaryErrorMessageFallback {...params} />}>
      <MermaidRenderer {...props} />
    </ErrorBoundary>
  );
}
