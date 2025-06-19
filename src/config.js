// src/config.js
import 'dotenv/config'; // Use this for ESM
import { RPC_URLS } from '@filoz/synapse-sdk';

const config = {
  port: process.env.PORT || 8080,
  db: {
    connectionString: process.env.POSTGRES_DSN,
  },
  synapse: {
    privateKey: process.env.SYNAPSE_PRIVATE_KEY,
    network: process.env.SYNAPSE_NETWORK || 'calibration',
    rpcUrl: process.env.SYNAPSE_RPC_URL,
  },
};

// --- Validation ---
if (!config.db.connectionString) {
  throw new Error("Missing required environment variable: POSTGRES_DSN");
}
if (!config.synapse.privateKey) {
  throw new Error("Missing required environment variable: SYNAPSE_PRIVATE_KEY");
}
if (config.synapse.privateKey.length !== 66) {
    throw new Error("Invalid SYNAPSE_PRIVATE_KEY format. Must be a 66-character hex string (e.g., 0x...).");
}
if (!config.synapse.rpcUrl) {
    // Set default RPC if not provided
    console.warn(`SYNAPSE_RPC_URL not set, using default for ${config.synapse.network}.`);
    config.synapse.rpcUrl = RPC_URLS[config.synapse.network]?.http;
    if (!config.synapse.rpcUrl) {
        throw new Error(`Invalid SYNAPSE_NETWORK: ${config.synapse.network}. Cannot find default RPC URL.`);
    }
}

export default config;