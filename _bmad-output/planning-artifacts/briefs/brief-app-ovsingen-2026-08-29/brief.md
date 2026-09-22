# Product Brief: G.E.A.R.

This document outlines the scope, objectives, and requirements for the **G.E.A.R.**, a modularized web application designed for mobile and desktop devices. The initial release focuses on building a standalone user directory and a tool maintenance/inspection scheduling module.

---

## 1. Overview & Objectives

In the German Federal Agency for Technical Relief (THW) Ortsverband Singen, maintaining operational readiness of tools and equipment is critical. This app aims to modernize and streamline tool inspections, ensuring that inspections are completed on time by qualified personnel and that administrators can easily monitor status.

### Strategic Goals
* **Operational Readiness**: Real-time tracking of tool inspection status to prevent out-of-date equipment usage.
* **Safety & Compliance**: Restrict tool inspections to users holding specific qualifications (e.g., chainsaw certificate).
* **Modular Foundation**: Establish a clean, standalone user management system that serves as a platform for future modules.
* **Legal Compliance**: Ensure full adherence to DSGVO (GDPR) standards for user data privacy.
* **Software Professionalism**: Build a highly professional codebase that is modularized, maintainable, extendable, and testable to ensure long-term sustainability.

---

## 2. Core Personas & Roles

* **Helfer*in (Volunteer/Helper)**: Standard users who can view tool statuses and perform tool inspections for which they are qualified.
* **Fuehrung (Leadership)**: Management users who can oversee all tool statuses, run reports, and review checklists.
* **Schirrmeister (Equipment Caretaker)**: Users who maintain the equipment catalogue and tool types and review inspection history — they can manage tools and tool types.
* **Admin (Administrator)**: Power users who can approve new accounts, assign users to groups, edit permission sets, add qualifications, and configure tool types/schedules.

---

## 3. Scope & Key Features (V1)

### A. Standalone User Directory & Authentication
* **Email Authentication**: Secure email-based login.
* **MFA Support**: Optional Multi-Factor Authentication (MFA) via authenticator app (TOTP) during login.
* **Self-Registration with Admin Approval**: Users can request account creation from the login page, but the account remains inactive until explicitly approved by an **Admin**.
* **Groups & Permissions**:
  * Users belong to user groups.
  * Permissions are grouped into Permission Groups (e.g., "Helfer*in", "Fuehrung", "Admin") and assigned to individual users or user groups.
* **Flexible User Attributes**: Users can have arbitrary, flexible attributes stored for customization/extension.

### B. Tool Maintenance Module
* **Tool & Tool Type Management**:
  * Tools are categorized by **Tool Type** (e.g., chainsaw, generator).
  * Each tool type defines the default inspection schedule (e.g., every 12 months) and the required qualification to inspect tools of that type.
  * Specific tools can override the default schedule with a custom inspection interval.
  * **Flexible Attributes**: Both Tools and Tool Types support flexible, custom attributes.
  * **Data Population**: Admins can import tools/types via CSV upload or create them manually in the admin panel.
* **Checklists vs. Pass/Fail**:
  * Each tool type defines whether inspections are a simple **Pass/Fail** or require a **Checklist**.
  * For checklist inspections, the system stores the individual result of every checklist item.
* **Qualified Inspections & Failure Handling**:
  * Users can only inspect a tool if they possess the qualification associated with the tool type.
  * If a tool fails inspection (either checklist or pass/fail), it is flagged as **"Out of Service"** (status attribute).
* **Color-Coded Status Dashboard**:
  * **Red**: Inspection due now (expired).
  * **Orange**: Upcoming inspection due within the next 2 weeks.
  * **Green**: OK (inspection current).

### C. Admin & Data Management Module
* **Admin Module Isolation**: The admin interface is built as a separate, isolated module within the application ecosystem. Access to this interface and its controls is strictly restricted to users with explicit Admin rights.
* **Configuration Management**: Control user approvals, groups, qualifications, tool types, and schedules.

---

## 4. Technical Requirements & Hosting

* **Database Persistence**: All application state, user configurations, and logs must be stored in a persistent database.
* **Database Schema Versioning**: Schema modifications must be managed via versioned database migrations to support future updates without data loss.
* **Containerization**: The entire application must be containerized (Docker) to ensure deployment portability.
* **Backups & Restore**: 
  * Regular, automated backups of the database and media files must be configurable (supporting multiple storage destinations).
  * A reliable restore function must be established from the outset.
* **DSGVO (GDPR) Compliance**: The application must incorporate mechanisms to ensure data compliance, including user access reports, account deletion workflows (right to be forgotten), and minimized data collection practices.

---

## 5. Non-Goals (Out of Scope for V1)

* **External Integrations**: No direct API integration with external THW tools (e.g., Helferportal, Hermine, Extranet).
* **Tool Reservations**: No booking, checkout, or reservation features.
* **Repair & Issues Tracking**: No workflow for tracking external repair tasks or parts ordering.
* **Automated Alerting**: No email or push notifications for upcoming/expired inspections (visual dashboard alerts only).
