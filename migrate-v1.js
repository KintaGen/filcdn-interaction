import { pool } from './src/services/db.js';

// This is a more advanced SQL query. It deletes rows from file_cids
// where the row's internal `ctid` (a unique identifier for every row in a table)
// is NOT in the list of the most recent `ctid`s for each distinct `cid`.
// This effectively keeps only the latest entry for each CID and deletes all older duplicates.
const cleanupSQL = `
  DELETE FROM file_cids a USING (
      SELECT MIN(ctid) as ctid, cid
      FROM file_cids 
      GROUP BY cid HAVING COUNT(*) > 1
  ) b
  WHERE a.cid = b.cid 
  AND a.ctid <> b.ctid;
`;

// The SQL command to add the UNIQUE constraint.
const addConstraintSQL = `
  ALTER TABLE file_cids
  ADD CONSTRAINT file_cids_cid_key UNIQUE (cid);
`;

async function runMigration() {
  console.log('[MIGRATE] Starting database schema cleanup and migration V2...');
  const client = await pool.connect();
  try {
    // Step 1: Clean up any existing duplicate CIDs
    console.log('[MIGRATE] Step 1: Cleaning up duplicate CIDs from "file_cids" table...');
    const result = await client.query(cleanupSQL);
    console.log(`[MIGRATE] ✅ Cleanup complete. ${result.rowCount} duplicate row(s) deleted.`);

    // Step 2: Add the UNIQUE constraint to the now-clean column
    console.log('[MIGRATE] Step 2: Applying UNIQUE constraint to "cid" column...');
    await client.query(addConstraintSQL);
    console.log('[MIGRATE] ✅ Migration successful. UNIQUE constraint added.');

  } catch (err) {
    // If the constraint already exists, the error code is '42P07' (duplicate_object)
    if (err.code === '42P07') {
      console.log('[MIGRATE] ✅ Migration unnecessary. Constraint already exists.');
    } else {
      console.error('[MIGRATE] ❌ Migration failed:', err);
      process.exit(1);
    }
  } finally {
    client.release();
    await pool.end();
  }
}

runMigration();