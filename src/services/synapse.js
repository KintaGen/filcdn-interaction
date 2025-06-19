// src/services/synapse.js
import { Synapse } from '@filoz/synapse-sdk';
import config from '../config.js';

// Create a single, reusable Synapse instance
let synapseInstance = null;

export async function getSynapse() {
  if (!synapseInstance) {
    console.log(`[SYNAPSE] Initializing Synapse SDK for network: ${config.synapse.network} with CDN enabled`);
    synapseInstance = await Synapse.create({
      privateKey: config.synapse.privateKey,
      rpcURL: config.synapse.rpcUrl,
      withCDN: true, // Crucial flag for FilCDN
    });
    console.log('[SYNAPSE] SDK Initialized.');
  }
  return synapseInstance;
}

/**
 * A high-level function to handle the entire upload process.
 * It creates a storage service (or reuses an existing one) and uploads the data.
 * @param {Buffer} dataBuffer The file data as a buffer.
 * @param {object} [options] Optional parameters.
 * @param {number} [options.proofSetId] An existing proof set ID to use.
 * @returns {Promise<{commp: string, size: number, proofSetId: number}>} The result of the upload.
 */
export async function uploadData(dataBuffer, options = {}) {
  const synapse = await getSynapse();

  console.log('[SYNAPSE] Creating/resolving storage service...');

  // The createStorage method also benefits from the withCDN flag,
  // ensuring it selects CDN-compatible providers.
  const storage = await synapse.createStorage({
    proofSetId: options.proofSetId, // Can be undefined
    withCDN: true, // Explicitly set here too for robustness
    callbacks: {
      onProviderSelected: (provider) => console.log(`[SYNAPSE] Provider selected: ${provider.owner}`),
      onProofSetResolved: (info) => console.log(`[SYNAPSE] Proof set resolved. ID: ${info.proofSetId}, Is Existing: ${info.isExisting}`),
      onProofSetCreationStarted: (tx) => console.log(`[SYNAPSE] New proof set creation Tx: ${tx.hash}`),
      onProofSetCreationProgress: (status) => console.log(`[SYNAPSE] Creation progress: Mined=${status.transactionMined}, Live=${status.proofSetLive}`),
    }
  });

  // Pre-flight check before uploading
  console.log('[SYNAPSE] Performing preflight check...');
  const preflight = await storage.preflightUpload(dataBuffer.length);
  if (!preflight.allowanceCheck.sufficient) {
    console.error('[SYNAPSE] Preflight check failed: Allowance not sufficient.');
    console.error(`  - Required: ${JSON.stringify(preflight.allowanceCheck.required)}, Has: ${JSON.stringify(preflight.allowanceCheck.current)}`);
    throw new Error('Allowance not sufficient to upload file. Please run the setup script or increase allowance via the web app.');
  }
  console.log('[SYNAPSE] Preflight check passed.');


  console.log(`[SYNAPSE] Starting upload of ${dataBuffer.length} bytes to proof set ${storage.proofSetId}...`);

  const uploadResult = await storage.upload(dataBuffer, {
    onUploadComplete: (commp) => console.log(`[SYNAPSE] Upload complete to provider. CommP: ${commp}`),
    onRootAdded: (tx) => {
      if (tx) {
        console.log(`[SYNAPSE] Root addition transaction sent: ${tx.hash}`);
      } else {
        console.log('[SYNAPSE] Root added to proof set (legacy server).');
      }
    },
    onRootConfirmed: (rootIds) => console.log(`[SYNAPSE] Root IDs confirmed on-chain: ${rootIds.join(', ')}`),
  });

  console.log(`[SYNAPSE] Successfully uploaded. CommP: ${uploadResult.commp}`);

  return {
    commp: uploadResult.commp.toString(),
    size: uploadResult.size,
    proofSetId: storage.proofSetId,
  };
}