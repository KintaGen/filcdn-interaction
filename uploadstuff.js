import { Synapse, RPC_URLS } from '@filoz/synapse-sdk'

// Helper for retrying async operations with delay
async function retryAsync(operationName, fn, attempts = 100, delayMs = 60000) {
  let lastError
  for (let i = 1; i <= attempts; i++) {
    try {
      console.log(`🔄 Attempt ${i}/${attempts}: ${operationName}`)
      return await fn()
    } catch (err) {
      lastError = err
      console.error(`❌ ${operationName} failed on attempt ${i}:`, err.message)
      console.error(err.stack)
      if (err.cause) console.error('   Cause stack:', err.cause.stack)
      if (i < attempts) {
        console.log(`⏳ Waiting ${delayMs / 1000}s before retrying ${operationName}...`)
        await new Promise(r => setTimeout(r, delayMs))
      }
    }
  }
  throw lastError
}

;(async () => {
  console.log('🔄 Starting Synapse SDK initialization...')
  const PRIVATE_KEY = '0xf3ef8d035f75b6e6e42baa33c08b6a4b03f0deb9dfa69228ca0fde30f6a713d1'
  const RPC_URL = RPC_URLS.calibration.websocket

  try {
    console.log(`🗝  Using privateKey: ${PRIVATE_KEY.slice(0, 6)}...`)
    console.log(`🌐 Connecting to RPC at ${RPC_URL}`)

    console.log('🔧 Initializing Synapse SDK with CDN...')
    const synapse = await Synapse.create({ withCDN: true, privateKey: PRIVATE_KEY, rpcURL: RPC_URL })
    console.log('✅ Synapse SDK initialized with CDN')

    // Create storage service with retry
    const storage = await retryAsync(
      'Synapse.createStorage',
      () => synapse.createStorage(),
      100,
      60000
    )
    console.log('✅ Storage service ready')
    console.debug('Storage internal:', storage)

    // Monkey-patch storage.upload for detailed logging
    const origUpload = storage.upload.bind(storage)
    storage.upload = async (payload) => {
      console.log('🔧 storage.upload called')
      console.log('   Payload length:', payload.length)
      try {
        const result = await origUpload(payload)
        console.log('✅ storage.upload result:', result)
        return result
      } catch (err) {
        console.error('❌ storage.upload error:', err.message)
        console.error(err.stack)
        if (err.cause) {
          console.error('   Cause message:', err.cause.message)
          console.error('   Cause stack:', err.cause.stack)
        }
        throw err
      }
    }

    // Prepare payload for upload
    const payload = new TextEncoder().encode(
      '🚀 Welcome to decentralized storage on Filecoin! Your data is safe here. 🌍'
    )
    console.log('📤 Payload ready, bytes:', payload.length)

    // Upload with retry
    const uploadResult = await retryAsync(
      'storage.upload',
      () => storage.upload(payload),
      100,
      60000
    )
    console.log(`✅ Upload success! CommP: ${uploadResult.commp}`)

    // Download flows with logging
    const providerData = await retryAsync(
      'storage.providerDownload',
      async () => {
        console.log('🔧 providerDownload for:', uploadResult.commp)
        const data = await storage.providerDownload(uploadResult.commp)
        console.log('✅ providerDownload bytes:', data.length)
        return data
      },
      100,
      10000
    )

    const anyProviderData = await retryAsync(
      'synapse.download',
      async () => {
        console.log('🔧 synapse.download for:', uploadResult.commp)
        const data = await synapse.download(uploadResult.commp)
        console.log('✅ synapse.download bytes:', data.length)
        return data
      },
      100,
      10000
    )

    console.log('🎉 All operations completed successfully')
  } catch (error) {
    console.error('❌ Fatal error:', error.message)
    console.error(error.stack)
    process.exit(1)
  }
})();
