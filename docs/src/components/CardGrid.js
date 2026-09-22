import React from 'react';

/**
 * CardGrid — a responsive grid of Cards (Docusaurus MDX component).
 * Usage: <CardGrid>{cards}</CardGrid>
 */
export default function CardGrid({ children }) {
  return <div className="gear-card-grid">{children}</div>;
}