#!/usr/bin/env python3
import os
import sys
import json
import time
import argparse
from pathlib import Path
from dataclasses import asdict

# Ensure the parent directory is in python path
sys.path.append(str(Path(__file__).resolve().parents[1]))

from opspilot.config import Settings
from opspilot.incidents import parse_logs, build_rca_provider, ExtractiveRCAProvider, GroqRCAProvider, RCA
from opspilot.domain import RetrievedChunk, RepositoryFile
from opspilot.fixes import FixContext, ProposedChange, EditInstruction, GroqFixProvider, FixProposalService

def parse_args():
    parser = argparse.ArgumentParser(description="OpsPilot Offline Incident Evaluation Runner")
    parser.add_argument("--mode", choices=["mock", "live"], default="mock",
                        help="mock: uses expected fixes from metadata; live: calls real LLMs")
    parser.add_argument("--provider", choices=["local", "groq"], default="local",
                        help="local: extractive/local models; groq: live Groq models")
    parser.add_argument("--output", default="eval/report.json",
                        help="path to write the compiled report.json")
    return parser.parse_args()

def load_fixtures():
    fixtures_dir = Path(__file__).resolve().parent / "fixtures"
    fixtures = []
    if not fixtures_dir.exists():
        print(f"Error: Fixtures directory not found at {fixtures_dir}")
        return fixtures

    for item in fixtures_dir.iterdir():
        if item.is_dir() and (item / "metadata.json").exists():
            with open(item / "metadata.json", "r") as f:
                metadata = json.load(f)
            fixtures.append((item, metadata))
    return fixtures

def main():
    args = parse_args()
    fixtures = load_fixtures()
    if not fixtures:
        print("No fixtures found to evaluate.")
        sys.exit(1)

    print(f"Starting Offline Evaluation (mode={args.mode}, provider={args.provider}) across {len(fixtures)} fixtures...")
    print("=" * 80)

    report_items = []
    total_passed = 0

    settings = Settings.from_env()

    for idx, (path, metadata) in enumerate(fixtures, start=1):
        print(f"\n[{idx}/{len(fixtures)}] Evaluating: {metadata['title']} ({metadata['id']})")
        
        # Load Logs
        logs_file_path = path / metadata["logs_file"]
        with open(logs_file_path, "r") as f:
            logs_content = f.read()

        start_time = time.time()

        # Step 1: Parse Logs
        parsed = parse_logs(logs_content)

        # Step 2: Simulate RAG retrieval using fixture source files
        evidence = []
        candidates = {}
        for rel_path, fixture_file in metadata["buggy_files"].items():
            full_source_path = path / rel_path
            if full_source_path.exists():
                with open(full_source_path, "r") as f:
                    content = f.read()
                candidates[rel_path] = content
                evidence.append(RetrievedChunk(
                    id=f"eval-{rel_path}",
                    file_path=rel_path,
                    text=content,
                    score=1.0,
                    start_line=1,
                    end_line=len(content.splitlines()) or 1
                ))

        # Step 3: Run RCA Provider
        rca_provider = None
        if args.mode == "live" and args.provider == "groq" and settings.groq_api_key:
            rca_provider = build_rca_provider(settings.groq_api_key, settings.groq_model)
        else:
            rca_provider = ExtractiveRCAProvider()

        rca = rca_provider.analyze(parsed, evidence)
        print(f"  -> Generated RCA: '{rca.title}' (Confidence: {rca.confidence})")

        # Step 4: Propose Fix
        instructions = []
        error = None
        
        context = FixContext(
            incident_id="eval-uuid",
            title=rca.title,
            root_cause=rca.root_cause,
            fix_recommendation=rca.fix_recommendation,
            confidence=rca.confidence,
            sanitized_logs=parsed.sanitized_content,
            owner="test_owner",
            repository="test_repo",
            branch="main",
            installation_id=1,
            indexed_commit_sha="eval-sha",
            evidence_paths=metadata["expected_citations"]
        )

        try:
            if args.mode == "live" and args.provider == "groq" and settings.groq_api_key:
                fix_provider = GroqFixProvider(settings.groq_api_key, settings.groq_model)
                instructions = fix_provider.propose(context, candidates)
            else:
                # Mock Mode: read expected fix from metadata.json
                expected = metadata["expected_fix"]
                instructions = [
                    EditInstruction(
                        path=expected["file"],
                        search=expected["search"],
                        replace=expected["replace"],
                        reason="Golden fix metadata"
                    )
                ]

            # Step 5: Verify proposed edits locally
            if not instructions:
                raise ValueError("No edit instructions returned")

            working = dict(candidates)
            for inst in instructions:
                # Security Check: Forbidden paths
                for pattern in metadata["forbidden_paths"]:
                    if pattern.endswith("*"):
                        prefix = pattern[:-1]
                        if inst.path.startswith(prefix):
                            raise ValueError(f"Security Policy Violation: Attempted to edit forbidden path: {inst.path}")
                    elif inst.path == pattern:
                        raise ValueError(f"Security Policy Violation: Attempted to edit forbidden path: {inst.path}")

                if inst.path not in candidates:
                    raise ValueError(f"Fix attempted to edit an unapproved path: {inst.path}")

                if working[inst.path].count(inst.search) != 1:
                    raise ValueError(f"Fix search anchor is not unique in {inst.path}")

                working[inst.path] = working[inst.path].replace(inst.search, inst.replace, 1)

            print("  -> Fix applied successfully! (Local Verification PASSED)")
            passed = True
            total_passed += 1
        except Exception as exc:
            print(f"  -> Evaluation FAILED: {exc}")
            error = str(exc)
            passed = False

        duration = time.time() - start_time

        report_items.append({
            "fixture_id": metadata["id"],
            "title": metadata["title"],
            "language": metadata["language"],
            "passed": passed,
            "latency_s": round(duration, 3),
            "rca": {
                "title": rca.title,
                "confidence": rca.confidence,
            },
            "proposed_edits": [
                {
                    "path": inst.path,
                    "search": inst.search,
                    "replace": inst.replace,
                    "reason": inst.reason
                } for inst in instructions
            ] if passed else [],
            "error": error
        })

    # Compile final metrics
    pass_rate = (total_passed / len(fixtures)) * 100
    report = {
        "timestamp": time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime()),
        "pass_rate": pass_rate,
        "total_fixtures": len(fixtures),
        "total_passed": total_passed,
        "mode": args.mode,
        "provider": args.provider,
        "results": report_items
    }

    # Write report.json
    output_path = Path(__file__).resolve().parents[1] / args.output
    output_path.parent.mkdir(parents=True, exist_ok=True)
    with open(output_path, "w") as f:
        json.dump(report, f, indent=2)

    print("\n" + "=" * 80)
    print("EVALUATION COMPLETE")
    print(f"Pass Rate: {pass_rate:.1f}% ({total_passed}/{len(fixtures)} passed)")
    print(f"Compiled results written to: {output_path}")
    print("=" * 80)

if __name__ == "__main__":
    main()
