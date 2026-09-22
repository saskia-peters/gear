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
      description="G.E.A.R. documentation — what it is, how to use it, and how it is built.">
      <HomepageHeader />
      <main>
        <div className="container">
          <h2>Worum geht es?</h2>
          <p>
            Die <b>G.E.A.R.</b> (Geräte-Einsatz-Assistenz &amp; Readiness)
            modernisiert die Geräteverwaltung des Ortsverbands Singen: jedes
            Gerät hat einen klaren Ampelfarben-Status, jede Prüfung wird von
            einer <b>qualifizierten</b> Person durchgeführt, und alles ist
            nachvollziehbar dokumentiert.
          </p>

          <h2>Nach Zielgruppe</h2>
          <CardGrid>
            <Card
              to="/docs/overview/what-is-gear"
              icon="🧭"
              title="Überblick"
              description="Was ist G.E.A.R.? — verständlich für alle"
            />
            <Card
              to="/docs/end-users"
              icon="🧑‍🔧"
              title="Für Endnutzer"
              description="Dashboard, Prüfungen, Verlauf, Konto"
            />
            <Card
              to="/docs/administrators"
              icon="🛠️"
              title="Für Administratoren"
              description="Benutzer, Rollen, Katalog, Einstellungen, Backups, DSGVO"
            />
            <Card
              to="/docs/developers"
              icon="💻"
              title="Für Entwickler"
              description="Module, API, Flows, Status, Epics"
            />
            <Card
              to="/docs/architects"
              icon="🏗️"
              title="Für Architekten"
              description="PRD, Architektur-Spine, Addendum, Roadmap, Deployment"
            />
            <Card
              to="/docs/overview/user-stories"
              icon="🎬"
              title="User Stories"
              description="G.E.A.R. in Aktion — mit echten Screenshots"
            />
          </CardGrid>

          <h2>Status &amp; API</h2>
          <CardGrid>
            <Card
              to="/docs/status"
              icon="📊"
              title="Implementierungsstatus"
              description="Automatisch berechneter Fortschritt"
            />
            <Card
              to="/docs/api/g-e-a-r-api"
              icon="🔌"
              title="API-Referenz"
              description="Interaktive OpenAPI-Dokumentation"
            />
            <Card
              to="/docs/epics"
              icon="🗂️"
              title="Epics &amp; Stories"
              description="Klickbare Übersicht aller Epics und Geschichten"
            />
          </CardGrid>
        </div>
      </main>
    </Layout>
  );
}