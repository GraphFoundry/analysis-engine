const { describe, it } = require('node:test');
const assert = require('node:assert');
const { parseTraceOptions } = require('../src/traceOptions');
const { createTrace } = require('../src/trace');

describe('Trace Options Parser', () => {
    it('should parse trace=true as boolean true', () => {
        const result = parseTraceOptions({ trace: 'true' });
        assert.strictEqual(result.trace, true);
    });

    it('should parse trace=1 as boolean true', () => {
        const result = parseTraceOptions({ trace: '1' });
        assert.strictEqual(result.trace, true);
    });

    it('should parse trace=false as boolean false', () => {
        const result = parseTraceOptions({ trace: 'false' });
        assert.strictEqual(result.trace, false);
    });

    it('should parse missing trace as boolean false', () => {
        const result = parseTraceOptions({});
        assert.strictEqual(result.trace, false);
    });

    it('should parse all trace options correctly', () => {
        const result = parseTraceOptions({
            trace: 'true',
            includeSnapshot: '1',
            includeRawPaths: 'false',
            includeEdgeDetails: true
        });
        assert.strictEqual(result.trace, true);
        assert.strictEqual(result.includeSnapshot, true);
        assert.strictEqual(result.includeRawPaths, false);
        assert.strictEqual(result.includeEdgeDetails, true);
    });

    it('should default all options to false when query is empty', () => {
        const result = parseTraceOptions({});
        assert.strictEqual(result.trace, false);
        assert.strictEqual(result.includeSnapshot, false);
        assert.strictEqual(result.includeRawPaths, false);
        assert.strictEqual(result.includeEdgeDetails, false);
    });
});

describe('Trace Helper - No-op Mode', () => {
    it('should return no-op API when trace is disabled', async () => {
        const trace = createTrace({ trace: false });

        let executed = false;
        const result = await trace.stage('test-stage', async () => {
            executed = true;
            return 'result';
        });

        assert.strictEqual(executed, true);
        assert.strictEqual(result, 'result');

        const finalized = trace.finalize();
        assert.strictEqual(finalized, null);
    });

    it('should not throw on addWarning when disabled', () => {
        const trace = createTrace({ trace: false });
        assert.doesNotThrow(() => {
            trace.addWarning('test-stage', 'warning');
        });
    });

    it('should not throw on setSummary when disabled', () => {
        const trace = createTrace({ trace: false });
        assert.doesNotThrow(() => {
            trace.setSummary('test-stage', { count: 10 });
        });
    });
});

describe('Trace Helper - Active Mode', () => {
    it('should capture stage timing when trace enabled', async () => {
        const trace = createTrace({ trace: true });

        await trace.stage('test-stage', async () => {
            // Simulate some work
            await new Promise(resolve => setTimeout(resolve, 10));
        });

        const result = trace.finalize();
        assert.notStrictEqual(result, null);
        assert.strictEqual(result.stages.length, 1);
        assert.strictEqual(result.stages[0].name, 'test-stage');
        assert.ok(result.stages[0].ms >= 10);
    });

    it('should include trace options in finalized result', () => {
        const traceOptions = {
            trace: true,
            includeSnapshot: true,
            includeRawPaths: false,
            includeEdgeDetails: false
        };
        const trace = createTrace(traceOptions);

        const result = trace.finalize();
        assert.deepStrictEqual(result.options, traceOptions);
    });

    it('should include generatedAt timestamp', () => {
        const trace = createTrace({ trace: true });
        const result = trace.finalize();

        assert.ok(result.generatedAt);
        assert.ok(new Date(result.generatedAt).toISOString());
    });

    it('should attach summary to stage', async () => {
        const trace = createTrace({ trace: true });

        await trace.stage('fetch-data', async () => {
            // Simulate work
        });

        trace.setSummary('fetch-data', { serviceCount: 12, edgeCount: 18 });

        const result = trace.finalize();
        assert.deepStrictEqual(result.stages[0].summary, {
            serviceCount: 12,
            edgeCount: 18
        });
    });

    it('should attach warnings to stage', async () => {
        const trace = createTrace({ trace: true });

        await trace.stage('process', async () => {
            // Simulate work
        });

        trace.addWarning('process', 'Data incomplete');
        trace.addWarning('process', 'Edge missing');

        const result = trace.finalize();
        assert.deepStrictEqual(result.stages[0].warnings, [
            'Data incomplete',
            'Edge missing'
        ]);
    });

    it('should handle multiple stages', async () => {
        const trace = createTrace({ trace: true });

        await trace.stage('stage1', async () => {
            await new Promise(resolve => setTimeout(resolve, 5));
        });

        await trace.stage('stage2', async () => {
            await new Promise(resolve => setTimeout(resolve, 5));
        });

        const result = trace.finalize();
        assert.strictEqual(result.stages.length, 2);
        assert.strictEqual(result.stages[0].name, 'stage1');
        assert.strictEqual(result.stages[1].name, 'stage2');
    });

    it('should return stage function result', async () => {
        const trace = createTrace({ trace: true });

        const result = await trace.stage('compute', async () => {
            return { value: 42 };
        });

        assert.deepStrictEqual(result, { value: 42 });
    });
});

describe('Trace Backward Compatibility', () => {
    it('should not affect response when trace is false', () => {
        const traceOptions = { trace: false };
        const trace = createTrace(traceOptions);

        const pipelineTrace = trace.finalize();

        // When trace is false, finalize returns null
        // This ensures backward compatibility: no pipelineTrace field added
        assert.strictEqual(pipelineTrace, null);
    });
});

describe('Pipeline Trace Integration', () => {
    it('should support multiple provider-level stages', async () => {
        const trace = createTrace({ trace: true });

        await trace.stage('staleness-check', async () => {
            // Simulate health check
        });
        
        await trace.stage('fetch-neighborhood', async () => {
            // Simulate fetch
        });
        
        await trace.stage('build-snapshot', async () => {
            // Simulate build
        });

        const result = trace.finalize();
        assert.strictEqual(result.stages.length, 3);
        assert.strictEqual(result.stages[0].name, 'staleness-check');
        assert.strictEqual(result.stages[1].name, 'fetch-neighborhood');
        assert.strictEqual(result.stages[2].name, 'build-snapshot');
    });

    it('should support scenario-parse stage', async () => {
        const trace = createTrace({ trace: true });

        await trace.stage('scenario-parse', async () => {
            return { serviceIdResolved: 'default:frontend', maxDepth: 2 };
        });

        trace.setSummary('scenario-parse', {
            serviceIdResolved: 'default:frontend',
            maxDepth: 2
        });

        const result = trace.finalize();
        assert.strictEqual(result.stages[0].name, 'scenario-parse');
        assert.ok(result.stages[0].summary);
        assert.strictEqual(result.stages[0].summary.serviceIdResolved, 'default:frontend');
        assert.strictEqual(result.stages[0].summary.maxDepth, 2);
    });

    it('should support simulation-level stages', async () => {
        const trace = createTrace({ trace: true });

        await trace.stage('path-analysis', async () => {});
        await trace.stage('compute-impact', async () => {});
        await trace.stage('recommendations', async () => {});

        trace.setSummary('path-analysis', { pathsFound: 10, pathsReturned: 5 });
        trace.setSummary('compute-impact', { affectedCallersCount: 3, totalLostTrafficRps: 150 });
        trace.setSummary('recommendations', { recommendationCount: 2 });

        const result = trace.finalize();
        assert.strictEqual(result.stages.length, 3);
        assert.ok(result.stages.every(s => s.summary));
    });
});
