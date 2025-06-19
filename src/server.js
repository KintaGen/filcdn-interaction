// src/server.js
import express from 'express';
import cors from 'cors';
import config from './config.js'; // Note the .js extension
import apiRoutes from './routes/api.js'; // Note the .js extension

const app = express();

// --- Middleware ---
app.use(cors());
app.use(express.json());
app.use(express.urlencoded({ extended: true }));

app.use((req, res, next) => {
    console.log(`[HTTP] ${req.method} ${req.url}`);
    next();
});

// --- Routes ---
app.get('/', (req, res) => {
  res.json({ message: 'Synapse JS Backend is running!' });
});

app.use('/api', apiRoutes);

// --- Error Handling ---
app.use((err, req, res, next) => {
    console.error('[GLOBAL ERROR]', err);
    const errorMessage = err.cause ? `${err.message}: ${err.cause.message}` : err.message;
    res.status(err.status || 500).json({
        error: errorMessage || 'An internal server error occurred.',
    });
});

// --- Start Server ---
app.listen(config.port, () => {
  console.log(`[START] Server listening on http://localhost:${config.port}`);
});