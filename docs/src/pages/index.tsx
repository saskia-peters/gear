import React, {type ReactNode} from 'react';
import clsx from 'clsx';
import Link from '@docusaurus/Link';
import useDocusaurusContext from '@docusaurus/useDocusaurusContext';
import Layout from '@theme/Layout';
import Heading from '@theme/Heading';

import CardGrid from '../components/CardGrid';
import Card from '../components/Card';

import styles from './index.module.css';

function HomepageHeader() {
  const {siteConfig} = useDocusaurusContext();
  return (
    <header className={clsx('hero hero--primary', styles.heroBanner)}>
      <div className="container">
        <Heading as="h1" className="hero__title">
          {siteConfig.title}
        </Heading>
        <p className="hero__subtitle">{siteConfig.tagline}</p>
        <div className={styles.buttons}>
          <Link className="button button--secondary button--lg" to="/docs/intro">
            Documentation starten
          </Link>
          <Link className="button button--secondary button--lg" to="/docs/status">
            Implementierungsstatus
          </Link>
        </div>
      </div>
    </header>
  );
}

export default function Home(): ReactNode {
  const {siteConfig} = useDocusaurusContext();
  return (
    <Layout
      title={`${siteConfig.title}`}
      description="G.E.A.R. documentation — planning, architecture, epics, status, and screenshots.">
      <HomepageHeader />
      <main>
        <div className="container">
          <h2>Worum geht es?</h2>
          <p>
            Die <b>G.E.A.R.</b> (Geräte-Einsatz-Assistenz &amp; Readiness)
            modernisiert die Geräteverwaltung des Ortsverbands Singen. Diese
            Doku bündelt Planung, Architektur, Epics, den Implementierungsstatus
            und Screenshots der laufenden Anwendung.
          </p>

          <h2>Dokumente</h2>
          <CardGrid>
            <Card
              to="/docs/intro"
              icon="🏠"
              title="Einführung"
              description="Dokumentation &amp; Technologie-Überblick"
            />
            <Card
              to="/docs/management-overview"
              icon="🧭"
              title="Management-Überblick"
              description="Verständlich für Entscheider und neue Mitglieder"
            />
            <Card
              to="/docs/epics/"
              icon="🗂️"
              title="Epics &amp; Stories"
              description="Klickbare Übersicht aller Epics und Geschichten"
            />
            <Card
              to="/docs/status"
              icon="📊"
              title="Implementierungsstatus"
              description="Automatisch berechneter Fortschritt"
            />
            <Card
              to="/docs/screenshots"
              icon="📷"
              title="Screenshots"
              description="Vorhandene und fehlende Aufnahmen (Desktop &amp; Mobil)"
            />
          </CardGrid>

          <h2>Planungsdokumente</h2>
          <CardGrid>
            <Card
              to="/docs/planning/prd"
              icon="📋"
              title="PRD"
              description="Funktionale Anforderungen, Epics, NFRs"
            />
            <Card
              to="/docs/planning/product-brief"
              icon="📄"
              title="Product Brief"
              description="Umfang, Ziele und Rahmen"
            />
            <Card
              to="/docs/planning/architecture-spine"
              icon="🏗️"
              title="Architektur-Spine"
              description="Technische Invarianten, ADs, Datenmodell"
            />
            <Card
              to="/docs/planning/addendum"
              icon="➕"
              title="Addendum"
              description="Technologie-Entscheidungen &amp; Deferrals"
            />
            <Card
              to="/docs/planning/ux-design"
              icon="🎨"
              title="UX-Design"
              description="Wireframes und UX-Konzept"
            />
          </CardGrid>
        </div>
      </main>
    </Layout>
  );
}