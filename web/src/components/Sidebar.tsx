import { NavLink } from 'react-router-dom'
import { getIsAdmin } from '../auth/authState.ts'
import styles from './Sidebar.module.css'

// Sidebar is the authenticated app shell's module navigation (Story 1.8): the
// "GEAR" module is always present; the "ADMIN" module is only shown when the
// server-authoritative is_admin flag is cached (the server resolves admin-group
// membership; the client never derives it). On narrow screens it collapses to a
// horizontal bar at the top.
export function Sidebar() {
  const isAdmin = getIsAdmin()

  return (
    <nav className={styles.sidebar} aria-label="Module">
      <ul className={styles.list}>
        <li>
          <NavLink
            to="/"
            end
            className={({ isActive }) => `${styles.link} ${isActive ? styles.active : ''}`}
          >
            GEAR
          </NavLink>
        </li>
        {isAdmin && (
          <li>
            <NavLink
              to="/admin"
              className={({ isActive }) => `${styles.link} ${isActive ? styles.active : ''}`}
            >
              ADMIN
            </NavLink>
          </li>
        )}
      </ul>
    </nav>
  )
}