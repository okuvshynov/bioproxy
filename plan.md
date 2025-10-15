In the experiments/bioproxy directory there's a basic implementation of a proxy server for LLM endpoint. The main usecase for it is warmup the KV cache for the messages with a given configured prefix.

Let's create a new version of it in the current directory (project/bioproxy). 

Process/engineering guidelines:
1. New version need to be a better and improved version specifically for llama.cpp.
2. It should also serve as a learning exercise - while being very experienced, I don't have much exposure to Golang so I'd like to make sure I understand everything you do. Comment your code extensively.
3. New version should be well-covered with unit and integration tests. For integration testing, we can use llama.cpp itself or create mock server.
4. We should start step-by step and use small basic changes.
5. From the beginning, use prometheus-like format for metric export endpoint
6. avoid external dependencies, implement as much as possible using golang stdlib
7. modularity: have separate modules, avoid long files with lots on functionality.
8. composition > inheritance. Avoid creating lots of abstractions. There's no need to create a base module for 'llm-server' if we ever would want to support more than just llama.cpp (say, lmstudio, vllm, sglang). Each implementation can live in a separate module, and common functionality (if any) can be extracted to common 'utils' module.
9. documentation. Should be complete, but fairly high-level. Let's write extensive, sometimes even excessive comments in the code.
10. Every step should be covered with tests, add appropriate metrics exported.
11. Every change should be small enough to understand easily.
12. Avoid creating empty directories - create directories only when you have files to put in them.

Product/feature requirements:
1. proxy should read a config file (with sensible default, say, ~/.config/bioproxy/conf.json
2. configuration should have mapping for prefix -> template file.
3. each config option can be overridden. I suggest we have default value (in code), which can be overridden by config value in config file, which can be overridden by command-line options. For example, we can have configuration for llm server address and port proxy itself listens on. For that port, default value in the code is 8081, config might have 8083, and we might supply '-port 8088' in the command line, and 8088 will be used
4. internally, proxy will iterate over all templates, fill them in with file content (same as current experimental proxy implementation), and if it has changed from the last update, mark them as 'warmup needed'
5. warmup process itself will become more complex. llama.cpp server (check llama.cpp/tools/server/README.md) supports save/load of kv cache to file. Whenever we plan to issue any request with template name foo, we will: 
5a. try loading kv cache from filename foo.bin. It might not exist yet, and we need to handle that.
5b. after that, run the query (warmup or user-initiated).
5c. after query is completed, save the kv cache to foo.bin
6. assume we work on slot # 0 all the time
7. maintain internal request queue and act as a gate for the llama.cpp server. Warmup queries should be issued only if there's no user-initiated query waiting. At the moment, llama.cpp server doesn't support request cancellation, but we might need to implement that in future.

Let's start with:
1. creating project structure
2. implementing template watch functionality (and config reading)
3. implementing metric export endpoint
4. implementing basic proxy functionality
5. implementing queues, warmup/user prioritization and plan ahead for warmup cancellation

