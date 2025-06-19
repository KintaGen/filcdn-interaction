// src/services/db.js
import pg from 'pg';
import config from '../config.js';

const { Pool } = pg;
export const pool = new Pool({
  connectionString: config.db.connectionString,
});

pool.on('connect', () => {
    console.log('[DB] Connected to PostgreSQL');
});

pool.on('error', (err, client) => {
    console.error('[DB] Unexpected error on idle client', err);
    process.exit(-1);
});

export const query = (text, params) => pool.query(text, params);