// Package core is the domain core of the Admin hexagon (Epic 3). It owns
// configuration logic over its three operational-settings tables and the
// DSGVO lifecycle orchestration (AD-8/AD-11/AD-14/AD-15/AD-16). Story 3.1
// materializes the SMTP-settings service (see settings.go); later stories
// (backup destinations, DSGVO) extend this package.
package core
