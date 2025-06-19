import express from 'express';
import multer from 'multer';
// Correctly import only the handlers that are actually exported
import { 
    uploadAndAddPaperHandler, 
    uploadAndAddGenomeHandler, 
    uploadAndAddSpectrumHandler 
} from '../controllers/upload.controller.js';

// These imports are for the data query endpoints and are correct
import { 
    queryDataHandler, 
    getDataByIDHandler, 
    listCIDsHandler 
} from '../controllers/data.controller.js';

const router = express.Router();
const storage = multer.memoryStorage();
const upload = multer({ storage });

// --- Specialized Upload Endpoints ---
// These are the primary endpoints. The logic now requires a proofSetID.
router.post('/upload/paper', upload.single('file'), uploadAndAddPaperHandler);
router.post('/upload/genome', upload.single('file'), uploadAndAddGenomeHandler);
router.post('/upload/spectrum', upload.single('file'), uploadAndAddSpectrumHandler);

// --- Generic Query Endpoints ---
router.get('/data/:type', queryDataHandler);
router.get('/data/:type/:cid', getDataByIDHandler);
router.get('/cids', listCIDsHandler);

// The old /proofset/upload-and-add-root route has been removed because its
// functionality is now part of the more specific handlers above.

export default router;