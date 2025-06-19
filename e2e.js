// e2e.js – pure JavaScript (ES Modules)
// ------------------------------------------------------------
//  Run with:  node e2e.js          (Node ≥ 18 — ESM by default)
// ------------------------------------------------------------
//  What’s inside
//    • Connect to the Filecoin calibration test‑net
//    • Deposit USDFC and approve the Pandora service
//    • Verbose logs so you can trace each step
//    • Dual lookup for Pandora address: numeric chainId → fallback name
// ------------------------------------------------------------

import { Synapse, RPC_URLS, TOKENS, CONTRACT_ADDRESSES } from '@filoz/synapse-sdk'
import { ethers } from 'ethers'

// -----------------------------------------------------------------------------
// 0 • Config  –––––––––––––––––––––––––––––––––––––––––––––––––––––––––––––––––––
const PRIVATE_KEY = process.env.PRIVATE_KEY ??
  '0xf3ef8d035f75b6e6e42baa33c08b6a4b03f0deb9dfa69228ca0fde30f6a713d1'
const RPC_URL     = RPC_URLS.calibration.websocket // Calibration test‑net
const DEPOSIT_AMT = '10'  // USDFC
const EPOCH_RATE  = '10'  // USDFC per epoch
const TOTAL_CAP   = '10'  // USDFC total lock‑up

// -----------------------------------------------------------------------------
// 1 • Init SDK  –––––––––––––––––––––––––––––––––––––––––––––––––––––––––––––––––
console.log('🔌  Connecting to Filecoin (test‑net)…')
const synapse = await Synapse.create({
  privateKey: PRIVATE_KEY,
  rpcURL: RPC_URL,
  withCDN: true
})

let chainId
if (typeof synapse.getChainId === 'function') {
  chainId = Number(synapse.getChainId())
  console.log(`✅  Connected • chainId = ${chainId}`)
} else if (synapse.provider && typeof synapse.provider.getNetwork === 'function') {
  const net = await synapse.provider.getNetwork()
  chainId   = Number(net.chainId)
  console.log(`✅  Connected • chainId = ${chainId} (via provider)`)  
} else {
  throw new Error('Unable to determine chainId – update the SDK')
}

const chainName = chainId === 314
  ? 'mainnet'
  : chainId === 314159
    ? 'calibration'
    : undefined

// -----------------------------------------------------------------------------
// 2 • Fund & approve payments  ––––––––––––––––––––––––––––––––––––––––––––––––––
console.log('💰  Depositing USDFC…')
await synapse.payments.deposit(ethers.parseUnits(DEPOSIT_AMT, 18), TOKENS.USDFC)
console.log('   ↳ deposit tx sent')

const pandoraAddress =
  (CONTRACT_ADDRESSES.PANDORA_SERVICE[chainId] ??
   CONTRACT_ADDRESSES.PANDORA_SERVICE[chainName] ??
   null)

if (!pandoraAddress) {
  throw new Error(`❌  Pandora address not found for chainId ${chainId}`)
}
console.log(`🔑  Approving Pandora payments @ ${pandoraAddress}`)

await synapse.payments.approveService(
  pandoraAddress,
  ethers.parseUnits(EPOCH_RATE, 18), // rate allowance
  ethers.parseUnits(TOTAL_CAP, 18)   // total cap
)
console.log('   ↳ approve tx sent')

// -----------------------------------------------------------------------------
// 3 • Done  ––––––––––––––––––––––––––––––––––––––––––––––––––––––––––––––––––––
console.log('\n🎉  Setup complete – you’re ready to upload!')
