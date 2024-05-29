/// <reference path="./.sst/platform/config.d.ts" />

export default $config({
  app(input) {
    return {
      name: "streaming-problem",
      removal: input?.stage === "production" ? "retain" : "remove",
      home: "aws",
      providers: {
        aws: {
          region: "us-west-1",
        },
      },
    };
  },
  async run() {
    const fn = new sst.aws.Function("MyFunction", {
      handler: "./src/streaming.handler",
      streaming: true,
      url: true,
      timeout: "15 minutes",
    });

    return {
      url: fn.url,
    };
  },
});
