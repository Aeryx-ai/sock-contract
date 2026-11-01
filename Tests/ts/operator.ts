// Type check only (make test): the generated service descriptor is what
// @connectrpc/connect v2 clients take, as the web console uses it.
import { create, toJsonString } from "@bufbuild/protobuf";
import { createClient, type Client, type Transport } from "@connectrpc/connect";
import { OperatorService, ResolveReportRequestSchema } from "../../gen/ts/sock/v1/operator_pb";
import { ResolutionOutcome } from "../../gen/ts/sock/v1/moderation_pb";

export function operatorClient(transport: Transport): Client<typeof OperatorService> {
  return createClient(OperatorService, transport);
}

export const resolveJSON: string = toJsonString(
  ResolveReportRequestSchema,
  create(ResolveReportRequestSchema, { reportId: "r1", outcome: ResolutionOutcome.OWNER_SUSPENDED }),
);
