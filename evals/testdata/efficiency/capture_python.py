"""Capture adk-python's EvalCaseResult for the efficiency metrics.

Builds one static eval case of two invocations and an inference result whose
invocations carry model usage, a tool call and turn durations, then scores it
with adk-python's LocalEvalService.evaluate and the default efficiency
metrics. No model is called and nothing touches the network.

Captured from adk-python 67b217d00 on 2026-11-10 with:

  uv run --no-project --with "/path/to/adk-python[eval]" \\
      python -I evals/testdata/efficiency/capture_python.py

Writes inference_input.json (the eval case and inference result) and
python_eval_case_result.json (the scored result, sessionDetails removed) next
to this script. Usage and duration values come from adk-python's
tests/unittests/evaluation/test_efficiency_evaluators.py (:298, :510, :828);
only non-zero usage fields are set, since Go reads a zero count as absent.
"""

import asyncio
import json
import pathlib

from google.adk.agents.base_agent import BaseAgent
from google.adk.evaluation.base_eval_service import EvaluateConfig
from google.adk.evaluation.base_eval_service import EvaluateRequest
from google.adk.evaluation.base_eval_service import InferenceResult
from google.adk.evaluation.base_eval_service import InferenceStatus
from google.adk.evaluation.eval_case import EvalCase
from google.adk.evaluation.eval_case import Invocation
from google.adk.evaluation.eval_case import InvocationEvent
from google.adk.evaluation.eval_case import InvocationEvents
from google.adk.evaluation.eval_config import append_default_efficiency_metrics
from google.adk.evaluation.in_memory_eval_sets_manager import InMemoryEvalSetsManager
from google.adk.evaluation.local_eval_service import LocalEvalService
from google.adk.sessions.in_memory_session_service import InMemorySessionService
from google.genai import types

HERE = pathlib.Path(__file__).resolve().parent
APP = "app"


class StubAgent(BaseAgent):
  """Never runs: evaluate scores the given inferences without the agent."""

  async def _run_async_impl(self, ctx):
    if False:
      yield


def text(role, value):
  return types.Content(role=role, parts=[types.Part(text=value)])


def main():
  golden = [
      Invocation(invocation_id="inv1", user_content=text("user", "turn one"), final_response=text("model", "one")),
      Invocation(invocation_id="inv2", user_content=text("user", "turn two"), final_response=text("model", "two")),
  ]
  case = EvalCase(eval_id="case1", conversation=golden)
  sets = InMemoryEvalSetsManager()
  sets.create_eval_set(APP, "es1")
  sets.add_eval_case(APP, "es1", case)

  inv1 = Invocation(
      invocation_id="inv1",
      user_content=text("user", "turn one"),
      final_response=text("model", "one"),
      intermediate_data=InvocationEvents(invocation_events=[
          InvocationEvent(
              author="agent",
              model_version="gemini-test",
              usage_metadata=types.GenerateContentResponseUsageMetadata(
                  prompt_token_count=700,
                  cached_content_token_count=100,
                  tool_use_prompt_token_count=20,
                  candidates_token_count=200,
                  thoughts_token_count=80,
              ),
          ),
          InvocationEvent(
              author="agent",
              content=types.Content(role="model", parts=[types.Part(function_call=types.FunctionCall(name="t1", args={}))]),
          ),
      ]),
      duration=10.0,
      model_duration=8.0,
      tool_duration=1.0,
  )
  inv2 = Invocation(
      invocation_id="inv2",
      user_content=text("user", "turn two"),
      final_response=text("model", "two"),
      intermediate_data=InvocationEvents(invocation_events=[
          InvocationEvent(
              author="agent",
              model_version="gemini-test",
              usage_metadata=types.GenerateContentResponseUsageMetadata(
                  prompt_token_count=200,
                  candidates_token_count=100,
              ),
          ),
      ]),
      duration=20.0,
      model_duration=10.0,
      tool_duration=5.0,
  )
  inference = InferenceResult(
      app_name=APP,
      eval_set_id="es1",
      eval_case_id="case1",
      inferences=[inv1, inv2],
      session_id="s-1",
      status=InferenceStatus.SUCCESS,
      repetition_index=1,
  )
  (HERE / "inference_input.json").write_text(json.dumps({
      "evalCase": case.model_dump(mode="json", by_alias=True, exclude_none=True),
      "inferenceResult": inference.model_dump(mode="json", by_alias=True, exclude_none=True),
  }, indent=2, sort_keys=True) + "\n")

  service = LocalEvalService(
      root_agent=StubAgent(name="agent"),
      eval_sets_manager=sets,
      session_service=InMemorySessionService(),
  )
  request = EvaluateRequest(
      inference_results=[inference],
      evaluate_config=EvaluateConfig(eval_metrics=append_default_efficiency_metrics([])),
  )

  async def run():
    return [r async for r in service.evaluate(request)]

  results = asyncio.run(run())
  assert len(results) == 1, results
  dumped = json.loads(results[0].model_dump_json(by_alias=True, exclude_none=True))
  dumped.pop("sessionDetails", None)
  (HERE / "python_eval_case_result.json").write_text(json.dumps(dumped, indent=2, sort_keys=True) + "\n")


if __name__ == "__main__":
  main()
