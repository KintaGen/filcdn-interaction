// src/init-db.js
import { pool } from './services/db.js';

const createTables = [
  // ... (SQL strings remain the same)
  `CREATE TABLE IF NOT EXISTS file_cids ( id SERIAL PRIMARY KEY, filename TEXT NOT NULL, cid TEXT NOT NULL UNIQUE, uploaded_at TIMESTAMPTZ DEFAULT NOW() );`,
  `CREATE TABLE IF NOT EXISTS paper ( cid TEXT PRIMARY KEY, title TEXT NOT NULL, journal TEXT, year INTEGER, keywords TEXT[], authors TEXT[], created_at TIMESTAMPTZ DEFAULT NOW() );`,
  `CREATE TABLE IF NOT EXISTS spectrum ( cid TEXT PRIMARY KEY, compound TEXT, technique_nmr_ir_ms TEXT, metadata_json JSONB, created_at TIMESTAMPTZ DEFAULT NOW() );`,
  `CREATE TABLE IF NOT EXISTS genome ( cid TEXT PRIMARY KEY, organism TEXT, assembly_version TEXT, notes TEXT, created_at TIMESTAMPTZ DEFAULT NOW() );`,
];


async function initializeDatabase() {
  console.log('[DB] Initializing database tables...');
  const client = await pool.connect();
  try {
    for (const sql of createTables) {
      await client.query(sql);
    }
    console.log('[DB] All tables created or already exist.');
  } catch (err) {
    console.error('[DB] Error creating tables:', err);
    process.exit(1);
  } finally {
    client.release();
    await pool.end(); // Ensure the script exits
  }
}

initializeDatabase();