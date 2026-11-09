"""Capture real adk-python dev-server eval responses as Go test fixtures.

Builds adk-python's DevServer in-process (in-memory session, artifact, memory
and credential services; loopback TestClient) and records the JSON its eval
routes answer, so the Go evals launcher can be tested against real shapes
instead of hand-written ones.

Captured from adk-python 67b217d00 on 2026-11-09 with:

  uv run --no-project --with "/path/to/adk-python[eval]" \
      python evals/testdata/adkpy/capture_adkpy.py run

Subcommands:
  run   POST adk-web main's run body to /dev/apps/test_app/eval-sets/set1/run
        and GET the stored result; writes run_eval_response.json and
        eval_set_result.json next to this script.
  metrics
        GET /dev/apps/test_app/metrics-info; writes metrics_info.json.

No model is called: the agent yields a fixed "ok" reply.
"""

import json
import pathlib
import sys
import tempfile

from fastapi.testclient import TestClient
from google.adk.agents.base_agent import BaseAgent
from google.adk.artifacts.in_memory_artifact_service import InMemoryArtifactService
from google.adk.auth.credential_service.in_memory_credential_service import InMemoryCredentialService
from google.adk.cli.dev_server import DevServer
from google.adk.evaluation.eval_case import EvalCase
from google.adk.evaluation.eval_case import Invocation
from google.adk.evaluation.eval_case import SessionInput
from google.adk.evaluation.in_memory_eval_sets_manager import InMemoryEvalSetsManager
from google.adk.evaluation.local_eval_set_results_manager import LocalEvalSetResultsManager
from google.adk.events.event import Event
from google.adk.memory.in_memory_memory_service import InMemoryMemoryService
from google.adk.sessions.in_memory_session_service import InMemorySessionService
from google.genai import types

HERE = pathlib.Path(__file__).resolve().parent
APP = "test_app"
LOOPBACK_BASE_URL = "http://127.0.0.1:8000"


class EchoAgent(BaseAgent):
  """Replies "ok" to every turn without calling a model."""

  async def _run_async_impl(self, ctx):
    yield Event(
        invocation_id=ctx.invocation_id,
        author=self.name,
        content=types.Content(role="model", parts=[types.Part(text="ok")]),
    )


class Loader:
  """Loads EchoAgent for every app name."""

  def __init__(self):
    self._agent = EchoAgent(name="echo_agent")

  def load_agent(self, app_name):
    return self._agent

  def list_agents(self):
    return [APP]

  def list_agents_detailed(self):
    return [{"name": APP, "root_agent_name": "echo_agent", "description": "",
             "language": "python", "is_computer_use": False}]


def _case(eval_id):
  return EvalCase(
      eval_id=eval_id,
      conversation=[
          Invocation(
              user_content=types.Content(role="user", parts=[types.Part(text="hi")]),
              final_response=types.Content(role="model", parts=[types.Part(text="ok")]),
          )
      ],
      session_input=SessionInput(app_name=APP, user_id="user-7", state={}),
  )


def _client(results_dir):
  sets = InMemoryEvalSetsManager()
  sets.create_eval_set(APP, "set1")
  sets.add_eval_case(APP, "set1", _case("case1"))
  sets.add_eval_case(APP, "set1", _case("case2"))
  server = DevServer(
      agent_loader=Loader(),
      session_service=InMemorySessionService(),
      memory_service=InMemoryMemoryService(),
      artifact_service=InMemoryArtifactService(),
      credential_service=InMemoryCredentialService(),
      eval_sets_manager=sets,
      eval_set_results_manager=LocalEvalSetResultsManager(agents_dir=results_dir),
      agents_dir=results_dir,
  )
  return TestClient(server.get_fast_api_app(), base_url=LOOPBACK_BASE_URL,
                    client=("127.0.0.1", 51234))


def _check(response, what):
  if response.status_code != 200:
    print(f"{what}: HTTP {response.status_code}: {response.text}", file=sys.stderr)
    sys.exit(1)
  return response.json()


def _write(name, value):
  (HERE / name).write_text(json.dumps(value, indent=2, sort_keys=True) + "\n")


def capture_run():
  with tempfile.TemporaryDirectory() as results_dir:
    client = _client(results_dir)
    # adk-web main's run body (src/app/core/services/eval.service.ts).
    run = _check(
        client.post(
            f"/dev/apps/{APP}/eval-sets/set1/run",
            json={"eval_case_ids": ["case2"],
                  "eval_metrics": [{"metricName": "response_match_score", "threshold": 0.7}]},
        ),
        "run",
    )
    _write("run_eval_response.json", run)
    listed = _check(client.get(f"/dev/apps/{APP}/eval-results"), "list eval results")
    # The hyphen route answers {"evalResultIds": [...]}; the legacy one a list.
    ids = listed["evalResultIds"] if isinstance(listed, dict) else listed
    if len(ids) != 1:
      print(f"want one eval result, got {ids}", file=sys.stderr)
      sys.exit(1)
    _write("eval_set_result.json",
           _check(client.get(f"/dev/apps/{APP}/eval-results/{ids[0]}"), "get eval result"))


def capture_metrics():
  with tempfile.TemporaryDirectory() as results_dir:
    client = _client(results_dir)
    _write("metrics_info.json",
           _check(client.get(f"/dev/apps/{APP}/metrics-info"), "metrics-info"))


if __name__ == "__main__":
  commands = {"run": capture_run, "metrics": capture_metrics}
  if len(sys.argv) != 2 or sys.argv[1] not in commands:
    print(f"usage: {sys.argv[0]} {{{'|'.join(commands)}}}", file=sys.stderr)
    sys.exit(2)
  commands[sys.argv[1]]()
