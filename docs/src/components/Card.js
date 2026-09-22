import React from 'react';
import Link from '@docusaurus/Link';

/**
 * Card — a topic navigation card.
 * Usage: <Card to="/docs/..." title="..." icon="📘" description="..."/>
 */
export default function Card({ to, title, icon, description }) {
  const content = (
    <>
      {icon && <span className="gear-card-icon">{icon}</span>}
      <span className="gear-card-title">{title}</span>
      {description && <span className="gear-card-desc">{description}</span>}
    </>
  );
  return to ? (
    <Link className="gear-card" to={to}>
      {content}
    </Link>
  ) : (
    <div className="gear-card">{content}</div>
  );
}