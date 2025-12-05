/**
 * Swagger UI Setup Module
 * 
 * Conditionally mounts Swagger UI at /api-docs when ENABLE_SWAGGER=true.
 * 
 * SAFETY:
 * - Disabled by default (must explicitly set ENABLE_SWAGGER=true)
 * - Dependencies are devDependencies only
 * - Dynamic require prevents crashes if deps missing in production
 * - Server continues running even if Swagger setup fails
 */

const path = require('path');
const fs = require('fs');

/**
 * Setup Swagger UI middleware on the Express app.
 * Only mounts if ENABLE_SWAGGER=true environment variable is set.
 * 
 * @param {import('express').Application} app - Express application instance
 */
function setupSwagger(app) {
    // SAFETY: Only enable when explicitly requested
    const enableSwagger = process.env.ENABLE_SWAGGER;
    const isEnabled = enableSwagger && ['true', '1', 'yes'].includes(String(enableSwagger).toLowerCase().trim());
    
    if (!isEnabled) {
        return;
    }

    try {
        // Dynamic require to avoid crash if deps not installed (production)
        let swaggerUi;
        let yaml;
        
        try {
            swaggerUi = require('swagger-ui-express');
        } catch (err) {
            console.error('[SWAGGER] swagger-ui-express not installed. Install with: npm install --save-dev swagger-ui-express');
            console.error('[SWAGGER] Swagger UI will not be available. Server continues without it.');
            return;
        }
        
        try {
            yaml = require('js-yaml');
        } catch (err) {
            console.error('[SWAGGER] js-yaml not installed. Install with: npm install --save-dev js-yaml');
            console.error('[SWAGGER] Swagger UI will not be available. Server continues without it.');
            return;
        }

        // Load OpenAPI spec
        const specPath = path.join(__dirname, '..', 'openapi.yaml');
        
        if (!fs.existsSync(specPath)) {
            console.error(`[SWAGGER] OpenAPI spec not found at: ${specPath}`);
            console.error('[SWAGGER] Swagger UI will not be available. Server continues without it.');
            return;
        }

        const specContent = fs.readFileSync(specPath, 'utf8');
        const swaggerDocument = yaml.load(specContent);

        // Swagger UI options
        const swaggerOptions = {
            explorer: true,
            customSiteTitle: 'Predictive Analysis Engine API',
            customCss: '.swagger-ui .topbar { display: none }',
            swaggerOptions: {
                persistAuthorization: true,
                displayRequestDuration: true
            }
        };

        // Mount Swagger UI at /swagger and /api-docs
        // Note: Each path needs its own serve middleware for static assets to work correctly
        app.use('/swagger', swaggerUi.serve, swaggerUi.setup(swaggerDocument, swaggerOptions));
        app.use('/api-docs', swaggerUi.serveFiles(swaggerDocument, swaggerOptions), swaggerUi.setup(swaggerDocument, swaggerOptions));

        console.log('[SWAGGER] Swagger UI enabled at /swagger and /api-docs');
    } catch (err) {
        // Catch-all: log error but don't crash server
        console.error('[SWAGGER] Failed to setup Swagger UI:', err.message);
        console.error('[SWAGGER] Server continues without Swagger UI.');
    }
}

module.exports = { setupSwagger };
