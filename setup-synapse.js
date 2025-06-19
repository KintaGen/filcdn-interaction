#!/usr/bin/env node

/**
 * FINAL, DEFINITIVE Wallet Setup & Diagnostic Script
 * This version includes exhaustive logging to show all existing proof sets
 * and their associated provider details.
 */

import { ethers } from 'ethers';
import { Synapse, TOKENS, CONTRACT_ADDRESSES, PandoraService } from '@filoz/synapse-sdk';
import 'dotenv/config'; 

// --- Configuration ---
const PRIVATE_KEY = process.env.SYNAPSE_PRIVATE_KEY;
const RPC_URL = process.env.SYNAPSE_RPC_URL || 'https://api.calibration.node.glif.io/rpc/v1';
const NETWORK_NAME = process.env.SYNAPSE_NETWORK || 'calibration';

if (!PRIVATE_KEY) throw new Error('ERROR: PRIVATE_KEY is required in your .env file.');

async function setupWallet() {
  try {
    console.log('--- Synapse Wallet Setup & Diagnostic ---');

    // Initialize SDK
    console.log('\n--- Step 1: Initializing Synapse SDK ---');
    const synapse = await Synapse.create({
      privateKey: PRIVATE_KEY,
      rpcURL: RPC_URL,
    });
    const signer = synapse.getSigner();
    const address = await signer.getAddress();
    console.log(`✓ Operating as wallet: ${address}`);

    //
    // ▼▼▼ DETAILED LOGGING ADDED HERE ▼▼▼
    //
    console.log('\n--- Step 2: Querying On-Chain State ---');
    const pandoraService = new PandoraService(synapse._provider, synapse.getPandoraAddress());

    // 2a. Fetch all available providers and fix their broken IDs
    const allProvidersRaw = await pandoraService.getAllApprovedProviders();
    const allProviders = allProvidersRaw.map((p, i) => ({ ...p, id: i + 1 }));
    console.log(`✓ Found ${allProviders.length} total approved providers on the network.`);

    // 2b. Fetch all of YOUR existing proof sets
    const existingProofsets = await pandoraService.getManagedProofSets(address);
    console.log(`✓ Wallet has ${existingProofsets.length} existing proof set(s).`);

    // 2c. Log a detailed summary table
    if (existingProofsets.length > 0) {
        console.log('\n--- Detailed Proof Set Summary ---');
        const summary = existingProofsets.map(set => {
            const providerInfo = allProviders.find(p => p.owner.toLowerCase() === set.payee.toLowerCase());
            return {
                "Proof Set ID": set.pdpVerifierProofSetId,
                "Provider Address (Payee)": set.payee,
                "Provider ID (Reconstructed)": providerInfo ? providerInfo.id : 'NOT FOUND',
                "Live": set.isLive,
                "Next Root ID": set.nextRootId,
            };
        });
        console.table(summary);
        console.log('------------------------------------');
    }
    //
    // ▲▲▲ END OF DETAILED LOGGING ▲▲▲
    //


    // Step 3: Check and grant Pandora Service Approval
    console.log('\n--- Step 3: Checking Pandora Service Approval ---');
    const pandoraAddress = CONTRACT_ADDRESSES.PANDORA_SERVICE[NETWORK_NAME];
    const serviceStatus = await synapse.payments.serviceApproval(pandoraAddress);
    const requiredLockup = ethers.parseUnits('1000', 18);

    console.log(`  - Is Approved: ${serviceStatus.isApproved}`);
    console.log(`  - Lockup Allowance: ${ethers.formatUnits(serviceStatus.lockupAllowance, 18)} USDFC`);

    if (!serviceStatus.isApproved || serviceStatus.lockupAllowance < requiredLockup) {
      console.log('Action Required: Approving Pandora service...');
      const rateAllowance = ethers.parseUnits('10', 18);
      await synapse.payments.approveService(pandoraAddress, rateAllowance, requiredLockup);
      console.log('✓ Pandora service approval transaction sent.');
    } else {
      console.log('✓ Pandora service allowance is already sufficient.');
    }

    // Step 4: Check and deposit USDFC if needed
    console.log('\n--- Step 4: Checking USDFC Deposit ---');
    const paymentsBalance = await synapse.payments.balance();
    console.log(`  - Deposited Balance: ${ethers.formatUnits(paymentsBalance, 18)} USDFC`);
    
    if (paymentsBalance < ethers.parseUnits('1', 18)) {
        console.log('Action Required: Depositing 10 USDFC...');
        const depositAmount = ethers.parseUnits('10', 18);
        await synapse.payments.deposit(depositAmount, TOKENS.USDFC);
        console.log('✓ USDFC deposit transaction sent.');
    } else {
        console.log('✓ Sufficient USDFC is already deposited.');
    }

    console.log('\n✅ --- Diagnostic Complete. ---');

  } catch (error) {
    console.error('\n❌ SCRIPT FAILED:', error.message);
    if (error.cause) console.error('  Caused by:', error.cause.message);
    process.exit(1);
  }
}

setupWallet();