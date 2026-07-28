'use client';

import { useEffect, useState } from 'react';
import { CheckCircle, XCircle, Clock, DollarSign, RefreshCw, FileCode, AlertTriangle, ShieldCheck } from 'lucide-react';

interface Edit {
  path: string;
  search: string;
  replace: string;
  reason: string;
}

interface EvalResult {
  fixture_id: string;
  title: string;
  language: string;
  passed: boolean;
  latency_s: number;
  rca: {
    title: string;
    confidence: string;
  };
  proposed_edits: Edit[];
  error: string | null;
}

interface EvalReport {
  timestamp: string;
  pass_rate: number;
  total_fixtures: number;
  total_passed: number;
  mode: string;
  provider: string;
  results: EvalResult[];
}

export default function EvalDashboard() {
  const [report, setReport] = useState<EvalReport | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [selectedResult, setSelectedResult] = useState<EvalResult | null>(null);

  const fetchReport = async () => {
    setLoading(true);
    setError(null);
    try {
      const res = await fetch('/api/v1/eval/report');
      if (res.status === 404) {
        setError('NO_REPORT');
        setReport(null);
        return;
      }
      if (!res.ok) {
        throw new Error('Failed to fetch evaluation report');
      }
      const data = await res.json();
      setReport(data);
    } catch (err: any) {
      setError(err.message || 'An error occurred while loading the report');
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    fetchReport();
  }, []);

  if (loading) {
    return (
      <div className="flex flex-col items-center justify-center min-h-[400px] gap-4">
        <RefreshCw className="animate-spin text-indigo-500" size={32} />
        <span className="text-[var(--text-secondary)] text-sm">Loading offline evaluation report...</span>
      </div>
    );
  }

  if (error === 'NO_REPORT') {
    return (
      <div className="max-w-2xl mx-auto mt-12 p-8 rounded-lg border border-[var(--border-light)] bg-[var(--bg-secondary)] flex flex-col items-center text-center gap-6">
        <div className="w-16 h-16 rounded-full bg-indigo-500/10 flex items-center justify-center">
          <FileCode className="text-indigo-500" size={32} />
        </div>
        <div>
          <h3 className="font-display font-bold text-xl mb-2 text-white">No Evaluation Report Found</h3>
          <p className="text-[var(--text-secondary)] text-sm max-w-md">
            The offline evaluation suite has not been run in this environment yet. Run the evaluation runner CLI from the root workspace directory to generate a report.
          </p>
        </div>
        <div className="w-full bg-[var(--bg-primary)] p-4 rounded-md font-mono text-left text-xs border border-[var(--border-light)] relative group">
          <span className="text-indigo-400"># Run the offline evaluation suite</span>
          <br />
          <span className="text-white">./ai-worker/.venv/bin/python ai-worker/eval/runner.py --mode mock</span>
        </div>
        <button
          onClick={fetchReport}
          className="px-4 py-2 rounded bg-indigo-600 text-white font-medium text-sm hover:bg-indigo-700 transition-colors"
        >
          Check Again
        </button>
      </div>
    );
  }

  if (error || !report) {
    return (
      <div className="max-w-md mx-auto mt-12 p-6 rounded-lg border border-red-500/20 bg-red-500/5 text-center flex flex-col items-center gap-4">
        <AlertTriangle className="text-red-500" size={32} />
        <h3 className="font-display font-bold text-lg text-white">Failed to Load Report</h3>
        <p className="text-[var(--text-secondary)] text-sm">{error}</p>
        <button
          onClick={fetchReport}
          className="px-4 py-2 rounded bg-[var(--bg-secondary)] text-white text-sm font-medium border border-[var(--border-light)] hover:bg-[#ffffff0d]"
        >
          Retry
        </button>
      </div>
    );
  }

  return (
    <div className="flex flex-col gap-8 max-w-7xl mx-auto">
      {/* Header section */}
      <div className="flex items-center justify-between border-b border-[var(--border-light)] pb-6">
        <div>
          <h1 className="font-display font-bold text-2xl text-white">Offline Evaluation Benchmark</h1>
          <p className="text-sm text-[var(--text-secondary)] mt-1">
            Last run: {new Date(report.timestamp).toLocaleString()} | Mode: <span className="capitalize font-semibold text-indigo-400">{report.mode}</span> | Provider: <span className="capitalize font-semibold text-indigo-400">{report.provider}</span>
          </p>
        </div>
        <button
          onClick={fetchReport}
          className="flex items-center gap-2 px-3 py-1.5 rounded-md bg-[var(--bg-secondary)] border border-[var(--border-light)] text-sm hover:bg-[#ffffff0d] transition-colors"
        >
          <RefreshCw size={14} />
          Reload
        </button>
      </div>

      {/* Metrics Cards */}
      <div className="grid grid-cols-1 md:grid-cols-4 gap-6">
        <div className="p-6 rounded-lg bg-[var(--bg-secondary)] border border-[var(--border-light)] relative overflow-hidden">
          <div className="absolute top-0 right-0 w-24 h-24 bg-indigo-500/5 rounded-full blur-2xl" />
          <span className="text-xs font-semibold text-[var(--text-secondary)] uppercase tracking-wider">Overall Pass Rate</span>
          <div className="flex items-baseline gap-2 mt-2">
            <span className={`text-4xl font-display font-bold ${report.pass_rate === 100 ? 'text-green-500' : 'text-indigo-400'}`}>
              {report.pass_rate.toFixed(0)}%
            </span>
          </div>
        </div>

        <div className="p-6 rounded-lg bg-[var(--bg-secondary)] border border-[var(--border-light)]">
          <span className="text-xs font-semibold text-[var(--text-secondary)] uppercase tracking-wider">Passed Incidents</span>
          <div className="flex items-center gap-3 mt-2">
            <span className="text-4xl font-display font-bold text-white">
              {report.total_passed}
            </span>
            <span className="text-lg text-[var(--text-secondary)]">/ {report.total_fixtures}</span>
          </div>
        </div>

        <div className="p-6 rounded-lg bg-[var(--bg-secondary)] border border-[var(--border-light)]">
          <span className="text-xs font-semibold text-[var(--text-secondary)] uppercase tracking-wider">Avg Latency</span>
          <div className="flex items-center gap-2 mt-2 text-white">
            <Clock className="text-indigo-400" size={24} />
            <span className="text-3xl font-display font-bold">
              {(report.results.reduce((acc, curr) => acc + curr.latency_s, 0) / report.total_fixtures).toFixed(3)}
            </span>
            <span className="text-sm text-[var(--text-secondary)]">s</span>
          </div>
        </div>

        <div className="p-6 rounded-lg bg-[var(--bg-secondary)] border border-[var(--border-light)]">
          <span className="text-xs font-semibold text-[var(--text-secondary)] uppercase tracking-wider">Security Policy</span>
          <div className="flex items-center gap-2 mt-2 text-green-500">
            <ShieldCheck size={24} />
            <span className="text-2xl font-display font-bold">Compliant</span>
          </div>
        </div>
      </div>

      {/* Main content table & details */}
      <div className="grid grid-cols-1 lg:grid-cols-3 gap-8">
        {/* Results List */}
        <div className="lg:col-span-2 flex flex-col gap-4">
          <h3 className="font-display font-bold text-lg text-white">Evaluated Incidents</h3>
          <div className="rounded-lg border border-[var(--border-light)] bg-[var(--bg-secondary)] overflow-hidden">
            <table className="w-full text-left border-collapse">
              <thead>
                <tr className="border-b border-[var(--border-light)] bg-[#ffffff05] text-[var(--text-secondary)] text-xs font-semibold uppercase tracking-wider">
                  <th className="p-4">Fixture ID / Title</th>
                  <th className="p-4">Language</th>
                  <th className="p-4">Status</th>
                  <th className="p-4 text-right">Action</th>
                </tr>
              </thead>
              <tbody className="divide-y divide-[var(--border-light)]">
                {report.results.map((result) => (
                  <tr key={result.fixture_id} className="hover:bg-[#ffffff05] transition-colors">
                    <td className="p-4">
                      <div className="font-medium text-white">{result.title}</div>
                      <div className="text-xs font-mono text-[var(--text-secondary)] mt-0.5">{result.fixture_id}</div>
                    </td>
                    <td className="p-4">
                      <span className="text-xs font-semibold uppercase text-indigo-400 bg-indigo-500/10 px-2 py-0.5 rounded">
                        {result.language}
                      </span>
                    </td>
                    <td className="p-4">
                      <div className="flex items-center gap-1.5">
                        {result.passed ? (
                          <>
                            <CheckCircle className="text-green-500" size={16} />
                            <span className="text-sm font-medium text-green-500">Passed</span>
                          </>
                        ) : (
                          <>
                            <XCircle className="text-red-500" size={16} />
                            <span className="text-sm font-medium text-red-500">Failed</span>
                          </>
                        )}
                      </div>
                    </td>
                    <td className="p-4 text-right">
                      <button
                        onClick={() => setSelectedResult(result)}
                        className="text-xs font-medium text-indigo-400 hover:text-indigo-300 transition-colors"
                      >
                        Inspect Details
                      </button>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </div>

        {/* Selected Result Detail panel */}
        <div className="flex flex-col gap-4">
          <h3 className="font-display font-bold text-lg text-white">Details & Proposed Code Diff</h3>
          {selectedResult ? (
            <div className="p-6 rounded-lg border border-[var(--border-light)] bg-[var(--bg-secondary)] flex flex-col gap-6 h-fit">
              <div>
                <h4 className="font-display font-bold text-base text-white">{selectedResult.title}</h4>
                <span className="text-xs font-mono text-[var(--text-secondary)]">{selectedResult.fixture_id}</span>
              </div>

              <div>
                <span className="text-xs font-semibold text-[var(--text-secondary)] uppercase tracking-wider block mb-1">Generated Root Cause (RCA)</span>
                <p className="text-sm text-white bg-[var(--bg-primary)] p-3 rounded border border-[var(--border-light)] leading-relaxed">
                  {selectedResult.rca.title}
                </p>
              </div>

              {selectedResult.passed && selectedResult.proposed_edits.length > 0 ? (
                <div className="flex flex-col gap-3">
                  <span className="text-xs font-semibold text-[var(--text-secondary)] uppercase tracking-wider block">Proposed Fixes</span>
                  {selectedResult.proposed_edits.map((edit, idx) => (
                    <div key={idx} className="flex flex-col gap-2 bg-[var(--bg-primary)] p-4 rounded border border-[var(--border-light)]">
                      <div className="flex justify-between items-center text-xs">
                        <span className="font-mono text-indigo-400">{edit.path}</span>
                        <span className="text-[var(--text-secondary)] italic">{edit.reason}</span>
                      </div>
                      <div className="font-mono text-xs mt-2 overflow-auto max-h-[160px] flex flex-col gap-1 border-l-2 border-red-500/50 pl-2 bg-red-500/5">
                        <div className="text-[var(--text-secondary)] text-[10px] uppercase font-sans tracking-wide">Original Code:</div>
                        <pre className="text-red-400 whitespace-pre">{edit.search}</pre>
                      </div>
                      <div className="font-mono text-xs mt-2 overflow-auto max-h-[160px] flex flex-col gap-1 border-l-2 border-green-500/50 pl-2 bg-green-500/5">
                        <div className="text-[var(--text-secondary)] text-[10px] uppercase font-sans tracking-wide">Patched Code:</div>
                        <pre className="text-green-400 whitespace-pre">{edit.replace}</pre>
                      </div>
                    </div>
                  ))}
                </div>
              ) : selectedResult.error ? (
                <div className="flex flex-col gap-1.5 p-4 rounded border border-red-500/20 bg-red-500/5 text-red-400">
                  <span className="text-xs font-semibold uppercase tracking-wider text-[var(--text-secondary)]">Error Log</span>
                  <p className="text-xs font-mono whitespace-pre-wrap">{selectedResult.error}</p>
                </div>
              ) : (
                <p className="text-sm text-[var(--text-secondary)] italic">No code fixes were proposed.</p>
              )}
            </div>
          ) : (
            <div className="rounded-lg border border-dashed border-[var(--border-light)] p-8 text-center text-[var(--text-secondary)] text-sm flex flex-col items-center justify-center min-h-[250px] bg-[var(--bg-secondary)]/30">
              Select an incident from the list to inspect the generated RCA, logs, and proposed diff changes.
            </div>
          )}
        </div>
      </div>
    </div>
  );
}
