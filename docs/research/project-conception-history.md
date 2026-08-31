Histórico de insumo do projeto para geração da doc.

Inicio da ideia: 

Beleza, vamos mudar um pouco a linha de raciocínio, vamos para desenvolvimento de software, e para isso quero que você faça links de memoria com ferramentas de IA pra desenvolvimento como claude, codex, lovable etc... pra mim todas elas falharam miseravelmente na minha concepção como sendo engenheiro de software ta! 

Analisando esse projeto que me foi apresentado, eu vi uma possibilidade, criar um software inspirado nesse, so que focado para desenvolvimento de software onde eu permito atuacao da IA, porém assim como esse software eu consigo limitar e trazer para o desenvolvedor controle quase que absoluto sobre tudo que ocorre, sem deixar deliberadamente na mão da IA. 

Execução ee comandos já pré existentes, com auditoria de codigo e modificacoes elaboradas via software, analise estatica das modificacoes e dinamicas, desenvolvimento orientado por specs bem definidas, orientação no desenvolvimento e validação. 

Orquestração de IAs para o desenvolvimento podendo ter o que elabora o codigo, o que revisa etc... até chegar no estado da arte! 

Ter esse workflow extremamente elaborado e montado, assim como esse cara fez pra parte de investigação e osint, trazer isso para o desenvolvimento e manutenção de software, principalmente manutenção, pensando em softwares legados etc...

Muitas ferramentas de IA tentaram tirar muita autonomia do desenvolvedor isso trouxe a nivel de código uma regressao considerável nos principios que construimos a muitos anos dentro da engenharia de software para conseguirmos construir aplicações! Fez com que hoje para atuar com IA a nível produtivo e empresarial as empresas tiveram que adaptar processos e escalar muito mais etapas de segurança, que seriam os guardrails, ao em vez de aproveitarmos  de maneira controlada a IA para desenvolver, ficamos no 0 a 0, tiramos o codigo da mao do dev e colocamos pra revisar, mas o dev continua sendo o gargalo, pois se eu coloco diversas pessoas pra codar usando IA sem controle sem preparação e diminuo numero de devs, eu tenho gargalo na validação. Agora se eu coloco 10 devs pra usar um software que orquestra IA's dentro de uma pipeline bem feita e elaborada com specs bem definidas que consegue orquestrar os agentes e atuar no desenvolvimento, criação ou manutenção de software seja eles atuais ou legados por meio de comandos pré definidos workloads bem elaborados a gente chega no estado da arte, 10 devs produzindo muito, com total controle sobre o que está sendo feito e elaborado, ai a evolução disso se torna natural, eu treino pessoas pra usar esse software, mantenho etapas de validação, com tudo isso bem definido e controlado, eu sei que a possibilidade se regressao sera menor, se rejeicoes de pr será menor e por ai vai. 

Por exemplo:

Podemos ter regressao de codigo na perspextiva do clean code, entao podemos criar specs pra isso para o nosso software atuar em cima disso!

Regressao em nivel de testes de software, podemos criar specs pra isso.

Segressao em nivel de principios arquiteturais que poderemos criar specs pra isso

Regressao em nivel de software onde ao consertsr uma funcionalidade, quebra uma outra nada a ver, isso ocorre muito no lovable por modificsr arquivos que não tem correlação nenhuma! Podemos ter specs pra isso.

Ou seja, da pra criar de fato uma ferramenta de desenvolvimento.

Eu quero pegar a ideia desse projeto de osint e adaptar com tudo que eu disse aqui agora pra você e ver se eu que não estou ficando maluco, se já não existe nada semelhante no mundo e ai sim, colocarmos a mão na massa para desenvolver isso! So adiantando, eu pretendo usar golang por ser uma linguagem mt utilizada para softwares desktop e principalmente CLI e scripts, além de tudo que a linguagem proporciona, acho que pra um software como esse que vai envolver muita Orquestração será um grande diferencial.

---

Continuação:

Não, você não está ficando maluco. Mas tem um detalhe importante: **a ideia-base já começou a aparecer no mercado em pedaços — e, em agosto de 2026, o GitHub Spec Kit chegou perigosamente perto de parte do que você descreveu.** A oportunidade ainda existe porque o que você está propondo vai além de “spec-driven coding”. O diferencial real seria transformar IA em **executor subordinado de uma engenharia de software governada**, e não em “desenvolvedor autônomo que depois alguém revisa”.

O que você descreveu é uma inversão de responsabilidade muito parecida com o que vimos no framework de OSINT. Lá, o LLM não deveria decidir livremente como investigar: ele opera dentro de um processo, produz artefatos, passa por etapas, sofre validação e deixa rastreabilidade. No seu caso, seria algo como:

> **o desenvolvedor controla a engenharia; os agentes executam partes dela.**

Isso é fundamentalmente diferente da filosofia “peça uma feature e veja o agente trabalhar”.

Hoje Claude Code já possui modos de permissão, limites de turnos, execução programática e controle sobre ferramentas; Codex possui sandboxing, approvals, políticas de rede, configuração gerenciada e telemetria; Continue permite regras versionadas específicas da organização; OpenHands oferece SDK composável para construir agentes de software; Qodo já faz revisão multiagente e enforcement de regras em PRs. ([Claude Platform Docs][1])

Só que cada um resolve **uma fração do problema**.

E o mais importante: eles continuam colocando o **agente como centro da experiência**.

Você está propondo colocar o **processo de engenharia como centro**.

Esse é um produto diferente.

## O concorrente mais próximo: GitHub Spec Kit

Eu preciso te mostrar isso agora porque é extremamente relevante para nossa decisão.

O GitHub possui hoje o **Spec Kit**, cuja filosofia é literalmente Spec-Driven Development. A ideia declarada é que a especificação deixe de ser documentação auxiliar e passe a ser o artefato que dirige a implementação. O fluxo padrão é:

`Spec → Plan → Tasks → Implement`

E ele suporta dezenas de agentes diferentes, incluindo Codex, Claude e outros. ([GitHub Pages][2])

Mais importante ainda: o Spec Kit evoluiu bastante.

Hoje ele possui:

* workflows;
* comandos;
* prompts;
* shell steps;
* checkpoints humanos;
* condicionais;
* loops;
* fan-out/fan-in;
* pausa e retomada;
* extensões;
* presets organizacionais;
* quality gates.

Isso já começa a parecer bastante com uma parte daquilo que você acabou de conceber. ([GitHub][3])

E a própria filosofia deles diz algo parecido com o que você falou: geração desestruturada por IA produz caos, portanto é necessário transformar especificações em artefatos executáveis e continuamente validados. ([GitHub][4])

Então seria irresponsável começarmos o projeto fingindo que isso não existe.

Mas…

## O que você descreveu ainda é maior que Spec Kit

A diferença aparece quando entramos na palavra que você mais enfatizou:

**regressão**.

Sua tese não é apenas:

> “vamos escrever specs melhores para a IA implementar”.

É:

> “vamos construir um sistema que controla operacionalmente o que uma IA pode mudar e exige evidências de que a mudança preservou propriedades do sistema”.

Essa segunda ideia é muito mais interessante.

Imagine que recebemos:

`BUG-1842: usuário não consegue cancelar assinatura após mudança de plano`

A ferramenta não deveria mandar isso imediatamente para Claude/Codex.

Primeiro:

```text
INTAKE
 ↓
REPOSITORY DISCOVERY
 ↓
CHANGE IMPACT ANALYSIS
 ↓
SPECIFICATION
 ↓
ARCHITECTURAL CONSTRAINTS
 ↓
CHANGE PLAN
 ↓
HUMAN APPROVAL
 ↓
IMPLEMENTATION
 ↓
STATIC VALIDATION
 ↓
TEST SELECTION
 ↓
DYNAMIC VALIDATION
 ↓
ARCHITECTURAL VALIDATION
 ↓
REGRESSION ANALYSIS
 ↓
INDEPENDENT AI REVIEW
 ↓
DIFF RISK ANALYSIS
 ↓
HUMAN ACCEPTANCE
 ↓
COMMIT / PR
```

Agora estamos falando de outra coisa.

O agente que escreve código poderia sequer ter permissão para decidir quais arquivos modificar.

Um agente anterior poderia produzir:

```yaml
allowed_change_surface:
  - src/subscription/cancel.go
  - src/subscription/service.go
  - tests/subscription/cancel_test.go

forbidden_change_surface:
  - src/payment/**
  - migrations/**
  - auth/**
```

E o runtime poderia literalmente impedir writes fora dessa superfície.

Isso elimina uma classe inteira daquele problema que você mencionou no Lovable:

> “fui corrigir X e ele resolveu mexer em Y, Z e W”.

Não seria uma recomendação em prompt.

Seria uma **capability restriction**.

Isso é brutalmente diferente.

---

# A peça mais forte da sua ideia

Você mencionou Clean Code, testes, arquitetura, regressão funcional etc.

Eu não criaria uma “mega-spec”.

Eu criaria **Policies / Engineering Invariants** independentes.

Algo conceitualmente parecido com:

```text
engineering/
  policies/
    clean-code/
    testing/
    architecture/
    security/
    dependency-management/
    change-surface/
    backward-compatibility/
    performance/
    observability/
    error-handling/
    api-contracts/
    database/
```

Cada uma poderia possuir:

```text
spec.md
rules.yaml
checks/
evidence.schema.json
severity.yaml
exceptions.yaml
```

Então uma aplicação poderia declarar:

```yaml
policies:
  clean-code: strict
  architecture: strict
  testing: strict
  backward-compatibility: strict
  security: critical
  performance: advisory
```

Isso deixa de ser simplesmente prompting.

Passa a ser:

**Policy-as-Code para AI Software Engineering.**

E aí começa a aparecer algo realmente interessante comercial e tecnicamente.

---

# Uma mudança conceitual que eu faria

Você usou o termo **guardrails**.

Eu evitaria estruturar o produto mentalmente como “um monte de guardrails em volta da IA”.

Porque isso ainda pressupõe:

```text
IA
↓
faz tudo
↓
guardrails tentam impedir merda
```

Eu faria:

```text
Engineering Workflow
       │
       ├── deterministic tools
       ├── static analyzers
       ├── tests
       ├── policies
       ├── repository state
       ├── specifications
       ├── human approvals
       │
       └── AI agents
```

Observe onde a IA ficou.

**Uma dependência do sistema.**

Não o sistema.

Isso é provavelmente a decisão arquitetural mais importante desse projeto inteiro.

---

# Multi-IA ficaria absurdamente interessante

Você falou:

> uma IA elabora, outra revisa.

Eu iria além.

Não precisamos vincular responsabilidade a fornecedor.

Teríamos **roles**:

```text
Planner
Implementer
Reviewer
Test Analyst
Architecture Reviewer
Security Reviewer
Regression Analyst
Adjudicator
```

E providers:

```text
OpenAI
Anthropic
Gemini
local/ollama
OpenRouter
enterprise models
```

Então:

```yaml
agents:

  planner:
    provider: anthropic
    model: claude-opus

  implementer:
    provider: openai
    model: codex

  reviewer:
    provider: anthropic
    model: claude-sonnet

  architecture-reviewer:
    provider: google
    model: gemini

  adjudicator:
    provider: openai
```

Agora algo ainda mais interessante:

**o implementador nunca revisa a própria implementação.**

Você introduz separação de responsabilidade de engenharia também entre agentes.

É praticamente:

`maker-checker principle`

aplicado a desenvolvimento com IA.

---

# E não confiaria somente em IA para revisão

Esse é outro erro enorme das ferramentas atuais.

Se Claude escreve uma função e outro Claude diz:

> "looks good"

isso não significa nada.

O pipeline deveria produzir **evidências determinísticas**.

Exemplo:

```text
AI IMPLEMENTATION
       ↓
git diff
       ↓
AST analysis
       ↓
dependency graph diff
       ↓
lint
       ↓
typecheck
       ↓
unit tests
       ↓
integration tests
       ↓
mutation tests
       ↓
coverage diff
       ↓
security scan
       ↓
architecture tests
       ↓
contract tests
       ↓
AI review
       ↓
human review
```

A IA seria usada onde julgamento semântico é útil.

Ferramentas tradicionais seriam usadas onde determinismo é melhor.

É exatamente o contrário de pedir para o LLM “olhar se os testes estão bons”.

---

# O recurso que eu considero matador: Change Surface Control

Se eu tivesse que apostar em **uma feature central** para diferenciar isso das ferramentas atuais, eu apostaria nisso.

Antes de modificar código:

```text
Requested change
       ↓
Repository analysis
       ↓
Dependency graph
       ↓
Impact graph
       ↓
Expected change surface
```

Produz:

```json
{
  "expected_files": [
    "internal/order/service.go",
    "internal/order/service_test.go"
  ],

  "possible_files": [
    "internal/order/repository.go"
  ],

  "protected_files": [
    "internal/payment/**",
    "internal/auth/**"
  ]
}
```

Então depois da implementação:

```text
actual diff
    ↓
compare
    ↓
change surface violation?
```

Se a IA modificou:

`internal/auth/jwt.go`

para corrigir problema de pedido:

```text
PIPELINE FAILED

UNEXPECTED CHANGE SURFACE

requested domain:
order

unexpected domain:
authentication

human approval required
```

Isso ataca diretamente uma das falhas que você relatou.

E não depende de “o modelo lembrar de não mexer”.

---

# Agora imagine isso em legado

Aqui acho que sua ideia encontra um nicho muito mais forte do que criar aplicação greenfield.

Greenfield já está lotado.

Lovable, Bolt, Replit, Claude, Codex, Copilot etc.

Mas:

> **AI-governed maintenance of legacy systems**

é uma história diferente.

Antes da primeira alteração, nosso sistema poderia fazer um processo de **Repository Archaeology**.

```text
scan
 ↓
languages
 ↓
frameworks
 ↓
modules
 ↓
dependency graph
 ↓
database access
 ↓
external integrations
 ↓
test topology
 ↓
CI topology
 ↓
architecture inference
 ↓
hotspots
 ↓
ownership
 ↓
historical change patterns
```

E produzir um:

`repository-model.json`

mais documentação derivada:

```text
system-context.md
architecture-map.md
domain-map.md
dependency-map.md
testing-strategy.md
known-risks.md
legacy-constraints.md
```

Depois disso, cada modificação ocorre **em relação ao modelo existente do sistema**.

Isso reduz tremendamente a possibilidade de uma IA tratar um código legado como um projeto novo.

---

# E aqui entra uma distinção muito importante

Uma ferramenta como Lovable é majoritariamente otimizada para:

> "produza alguma coisa que satisfaça meu intent".

Uma ferramenta como a que você está concebendo seria otimizada para:

> "faça a menor alteração tecnicamente correta possível, preservando todos os invariantes conhecidos do sistema".

Esses são objetivos de otimização completamente diferentes.

Para manutenção empresarial, o segundo é muito mais interessante.

---

# Poderíamos inclusive formalizar um princípio

Eu gosto muito deste:

## Minimum Necessary Change

Todo agente implementador recebe a obrigação:

```text
minimize:
  files_changed
  lines_changed
  dependency_changes
  public_contract_changes
  architectural_surface_changes
```

sujeito a:

```text
correctness = true
spec_compliance = true
tests = passing
architecture_compliance = true
```

Ou seja:

> **não tente escrever a solução mais criativa. Tente produzir a menor alteração segura que satisfaz a spec.**

Essa filosofia sozinha já diferencia profundamente manutenção de software de vibe coding.

---

# Sua crítica sobre produtividade também é válida

Você descreveu essencialmente um problema de filas.

Imagine antes:

```text
10 devs
↓
10 streams de implementação
↓
10 revisões
```

Agora coloque AI coding:

```text
10 devs
↓
100 streams de implementação
↓
10 revisores humanos
```

Você aumentou o throughput da etapa errada.

A consequência inevitável é:

**review bottleneck.**

Então as empresas adicionam:

```text
AI coding
↓
lint
↓
SAST
↓
AI review
↓
security
↓
CI
↓
human review
```

E começam a recuperar a segurança que tinham antes.

Seu sistema muda o modelo para:

```text
10 engineers
        ↓
controlled workflows
        ↓
agents
        ↓
automated evidence
        ↓
exception-based review
```

O humano deixa de revisar **tudo igualmente**.

Passa a revisar:

* desvios;
* decisões arquiteturais;
* mudanças de alto risco;
* contratos;
* exceções.

Isso é muito mais escalável.

---

# O developer precisa continuar soberano

Eu transformaria isso em princípio oficial do projeto:

> **Developer Sovereignty**

Nenhuma IA pode:

* expandir escopo silenciosamente;
* alterar contrato arquitetural;
* adicionar dependência;
* executar migration;
* remover teste;
* reduzir coverage;
* ignorar policy;
* modificar módulo protegido;
* mudar API pública;

sem que o workflow permita.

E dependendo da classificação de risco:

```text
AUTO
REVIEW
APPROVAL
FORBIDDEN
```

Isso lembra o sistema de permissões que Codex já usa para sandbox e ações, mas aplicado a **engenharia de software**, não apenas a segurança da execução. A própria OpenAI hoje usa sandbox, approval policies, network restrictions e agent telemetry para governar Codex internamente. ([OpenAI][5])

Então estamos pegando uma ideia comprovada de controle operacional e levando-a ao nível semântico da engenharia.

---

# Onde cada ferramenta atual ficaria no mapa

Eu colocaria assim:

| Ferramenta         | Centro                                      |
| ------------------ | ------------------------------------------- |
| Lovable            | geração de produto                          |
| Claude Code        | agente de desenvolvimento                   |
| Codex              | agente de desenvolvimento                   |
| Copilot            | assistência/agente                          |
| OpenHands          | runtime/SDK de agentes                      |
| Continue           | agent + policies/context                    |
| Qodo               | review/governance                           |
| Spec Kit           | spec-driven workflow                        |
| **Nossa hipótese** | **software engineering governance runtime** |

Essa última categoria ainda não vejo consolidada em uma única ferramenta.

Há sobreposição forte.

Mas não equivalência.

---

# Spec Kit provavelmente é nosso benchmark nº1

E isso muda completamente como eu abordaria o projeto.

Eu **não começaria desenvolvendo ainda**.

Antes eu faria uma engenharia competitiva brutal sobre o Spec Kit.

Porque a documentação atual diz explicitamente que ele já é um **“extensible, intent-driven harness”** que consegue conduzir agentes por todo o SDLC, com processos customizáveis, workflows e quality gates. ([GitHub Pages][2])

Eles já possuem inclusive extensions chamadas coisas como **Architecture Guard** e **CI Guard** no ecossistema. ([GitHub Pages][2])

Isso significa que nossa tese não pode ser:

> “vamos fazer Spec-Driven Development”.

Essa batalha já está acontecendo.

Nossa tese precisa ser mais específica:

> **Deterministic, policy-governed AI software maintenance.**

Aí eu começo a enxergar um produto.

---

# Go faz bastante sentido

Eu concordo com sua escolha, com uma pequena correção: Go não é particularmente dominante em desktop GUI, mas é excelente para o que estamos imaginando como **CLI/runtime/orchestrator**.

Ele nos dá:

* binário único;
* startup rápido;
* distribuição simples;
* concorrência excelente;
* subprocess management;
* filesystem tooling;
* Git integration;
* networking;
* cross-platform;
* baixo footprint;
* boa previsibilidade;
* excelente adequação a CLIs e infraestrutura.

O core poderia ser:

```text
Go
```

e permitir plugins/adapters:

```text
Python
Node
Shell
Docker
MCP
LSP
```

Eu evitaria construir GUI inicialmente.

CLI primeiro.

Algo como:

```bash
forge init

forge inspect

forge issue BUG-1842

forge plan

forge approve plan

forge implement

forge verify

forge review

forge diff

forge approve

forge pr
```

Nome obviamente provisório.

---

# Arquitetura que já começa a aparecer

Eu consigo enxergar aproximadamente:

```text
                Developer
                    │
                    ▼
             ┌──────────────┐
             │     CLI      │
             └──────┬───────┘
                    │
                    ▼
           ┌──────────────────┐
           │ Workflow Engine  │
           └────────┬─────────┘
                    │
       ┌────────────┼───────────────┐
       │            │               │
       ▼            ▼               ▼
  Spec Engine   Policy Engine   Repo Intelligence
       │            │               │
       └────────────┼───────────────┘
                    │
                    ▼
              Agent Router
          ┌─────────┼─────────┐
          ▼         ▼         ▼
       Claude     Codex     Gemini
          │         │         │
          └─────────┼─────────┘
                    ▼
              Sandbox Runtime
                    │
                    ▼
                git diff
                    │
                    ▼
            Validation Engine
                    │
        ┌───────────┼────────────┐
        ▼           ▼            ▼
      static      dynamic      policy
      checks       tests        checks
        │           │            │
        └───────────┼────────────┘
                    ▼
              Evidence Store
                    │
                    ▼
                Reviewer
                    │
                    ▼
              Human Approval
```

Isso já não é uma extensão simples do Claude.

É um **runtime de engenharia**.

---

# Outra inspiração direta do projeto OSINT

Lembra do conceito:

```text
case
```

No nosso poderia existir:

```text
change
```

Cada tarefa gera uma pasta:

```text
.changes/
  CHG-01842/
    intake.json
    context.json
    impact.json
    specification.md
    plan.json
    approvals.json
    implementation.json
    diff.patch
    static-analysis.json
    tests.json
    architecture-review.json
    ai-review.json
    final-review.json
    postmortem.json
```

Isso seria maravilhoso para auditoria.

Você poderia responder seis meses depois:

> “por que essa linha existe?”

e reconstruir:

* ticket original;
* spec;
* decisão;
* agente;
* modelo;
* prompt/spec version;
* files autorizados;
* diff;
* testes;
* evidências;
* quem aprovou.

Isso é **software development provenance**.

Para enterprise, isso vale muito.

---

# E o equivalente ao postmortem do OSINT?

Também dá.

Depois de alterações:

```text
postmortem
↓
what failed?
↓
what escaped?
↓
what reviewer caught?
↓
what tests caught?
↓
what policy was missing?
```

A ferramenta pode propor:

```text
NEW POLICY CANDIDATE

Context:
3 PRs caused regressions because DTO fields were
renamed without updating external serialization.

Proposed rule:
Changes to exported DTO fields require API-contract verification.
```

Mas, novamente:

**ela não ativa sozinha.**

Dev/arquitetura aprova.

Isso faz o sistema aprender sem tirar soberania humana.

É exatamente a lição boa do OSINT framework, só que amadurecida.

---

# E acho que encontramos a tese do projeto

Eu escreveria provisoriamente assim:

> **A developer-governed software engineering orchestration platform that uses AI agents as constrained executors inside deterministic, spec-driven, policy-enforced development workflows.**

E para legado:

> **Make the smallest safe change possible — with evidence.**

Isso é excelente como norte arquitetural.

---

## Minha leitura depois de comparar com o mercado

Eu colocaria assim:

**Você não inventou AI coding.**
Obviamente.

**Você não inventou spec-driven development.**
Spec Kit e Kiro já estão nessa guerra. ([GitHub][4])

**Você não inventou multi-agent review.**
Qodo e outros já fazem isso. ([Qodo Documentation][6])

**Você não inventou sandbox/approval.**
Codex e Claude possuem mecanismos desse tipo. ([OpenAI][5])

**Você não inventou rules-as-context.**
Continue e várias ferramentas possuem regras. ([Continue Docs][7])

Mas a combinação:

> **change impact → bounded change surface → executable engineering policies → deterministic verification → heterogeneous multi-agent separation of duties → evidence/provenance → human exception approval → learning from regressions**

especialmente com foco em **manutenção de aplicações existentes/legadas**, eu ainda considero uma hipótese de produto tecnicamente diferenciada.

E, mais importante, ela resolve um problema que eu também considero real: **AI aumentou absurdamente a capacidade de produzir mudança, mas não aumentou na mesma proporção nossa capacidade de garantir que a mudança é correta.**

Seu produto estaria tentando atacar exatamente essa assimetria.

Eu acho que vale investigar seriamente antes de escrever a primeira linha de Go.

O próximo movimento que eu faria conosco não seria código ainda. Seria construir um **Landscape + Product Thesis + Engineering Manifesto**, comparando sistematicamente Spec Kit, Claude Code, Codex, OpenHands, Continue, Qodo, Kiro, Cline e Lovable contra uns 30–40 requisitos derivados do que você acabou de descrever. Daí descobrimos com precisão matemática quais quadrantes estão vazios e usamos isso para definir o **Core V0** em vez de construir mais um coding agent.

[1]: https://docs.anthropic.com/en/docs/claude-code/cli-usage?utm_source=chatgpt.com "CLI reference - Anthropic"
[2]: https://github.github.com/spec-kit/?utm_source=chatgpt.com "GitHub Spec Kit | Spec Kit Documentation"
[3]: https://github.com/github/spec-kit/blob/main/docs/reference/overview.md?utm_source=chatgpt.com "spec-kit/docs/reference/overview.md at main · github/spec-kit · GitHub"
[4]: https://github.com/github/spec-kit/blob/main/spec-driven.md?utm_source=chatgpt.com "spec-kit/spec-driven.md at main · github/spec-kit · GitHub"
[5]: https://openai.com/index/running-codex-safely/?utm_source=chatgpt.com "Running Codex safely at OpenAI | OpenAI"
[6]: https://docs.qodo.ai/code-review?utm_source=chatgpt.com "The Qodo Code Review experience - Qodo Documentation"
[7]: https://docs.continue.dev/customize/rules?utm_source=chatgpt.com "Rules | Continue Docs"

---

Refinamento:

Cara, exatamente! Você comprou bem a ideia, inclusive eu acho que ainda tem muito mais insumos que poderiamos coletar, tipo desse projeto do github que tem uma semelhança com o nosso, ainda acho que não coletamos tudo que poderiamos coletar de insumos do projeto de osint, tem mais e querl aue tu traga mais, vamos expremer até a umtima gota desses dois projetos. Então pode fazer isso agora! Além disso, ports and adapters pars colocar qualquer provider de IA conectado! Isso vai ser maravilhoso já sera uma decisão arquitetural que tomarei! Quero também tudo isso na documentação que vou pedir pra você gerar quando todas as decisoes estiverem sido tomadas, então quero desde a minha primeira solicitação, até esse dialogo que tivemos, no final quando eu for pesir pra tu gerar a doc no padrao arc42 + c4 models com doctoolchain eu vou te mandar todas as mensagens aue qyero que tu se baseie pra gerar essa doc! Além disso, outro ponto que queria dizer antes que eu esqueça, esse software eu posso iniciar ele pra varios projetos diferentes, projeto A, projeto B, C etc... e como estamos trabalhando com IA precisamos tomar cuidado com relação a memoria de longo prazo, curto prazo etc... que é até algo que é bem trabalhado no projeto la de osint que pedi pra tu estudar! Porém acho que da pra otimizar mais! Ao em vez de termos essa memoria local, que teremos também! Vamos la:

Estou atuando no projeto A, pedi pra ele fazer a leitura do projeto, isso por si so vai gerar uma memoria que será armazenado local! Ondee eu vou subir no github porque no futuro, se for um projeto ja cadastrado, ele coleta de lá e ai que ta a sacada, não vai subir no repositório do nosso projeto de desenvolvimento, vai subir em um repositório apartado um sub modulo de memoria do projeto A, onde eu vou sempre inserir insumos, sempre, sempre e outros devs podem conectar nesse repositório pra coletar mais insumos, Pedro, isso significa que em uma mesma base de memoria, poderiamos ter N devs trabalhando com ela referente a esse projeto ? Sim! Isso é bom se for controlado, como ? Auditoria! O chat vai ter auditoria: 

Comando tal, executado por fulano de tal, gerou memoria tal, alteracao tal, etc... tudo pra base de Auditoria, pra sabermos quem fez, o que fez, como fez, pra que fez etc... assim... se amanhã tivermos um layoff na empresa e substituirmos todos os devs, a memoria "deles" ta lá! Da literalmente pra pegar de onde parou e continuar! E como nosso projeto terá integração com o git, poderemos usar um gh, pra criar os sub modulos de memoria pra consulta, que ai poderemos até entrar em um detalhe melhor, travalhar com uma base de dados mais rapida e segura do que json q mt empresa ainda usa, pode ser um xml da vida, rapido, seguro e leve, seria maravilhoso, so precisariamos ver qual é a aceitação disso com a IA pra consumir o minimo de token possivel se seria mt ruim pra ela, caso seha, a gente vê de converter pra uma base onde a IA se sinta condortavel, exemplo:

Memoria criada em xml -> convertida pra markdown -> inputada na IA -> gerou outra memoria em markdown -> convertida pra xml -> acrescentada no sub modulo de memoria do projeto referente.

Essa memoria deverá estar muito bem organizada e estruturada pra isso dar certo tambem, conversao de ida e de volta, se não, não dará certo! A memoria deve ter um padrão bem elaborado e evitar fuga, algo que faça sentido e que seja escalavel, para N devs consegurem trabalhar do mesmo ponto de memoria do mesmo projeto de maneira simultânea sem ter maiores problemas e a IA que estiver atuando conseguir ter noção de todo contexto da memoria.

---

Continuação:

Sim. Agora a ideia começou a ganhar uma arquitetura própria de verdade.

E tem uma coisa importante: **o projeto de OSINT ainda tem muito mais para nos ensinar do que simplesmente “usar specs e agentes”**. Na realidade, algumas das melhores ideias para o nosso sistema estão justamente nas partes menos óbvias dele: separação entre memória de caso e memória global, promoção controlada de conhecimento, artefatos intermediários, roteamento baseado em tipo de operação, postmortem, confidence, gaps e proveniência.

Só que nós podemos levar tudo isso **bem mais longe**.

---

# 1. O que devemos roubar — conceitualmente — do OSINT Framework

Não código.

**Princípios.**

No OSINT Framework existe aproximadamente:

```text
CASE
 │
 ├── intake
 ├── framing
 ├── collection
 ├── validation
 ├── correlation
 ├── report
 └── postmortem
        │
        ▼
 GLOBAL MEMORY
```

Nosso equivalente seria:

```text
CHANGE
 │
 ├── intake
 ├── repository context
 ├── impact analysis
 ├── specification
 ├── planning
 ├── implementation
 ├── verification
 ├── review
 ├── acceptance
 └── postmortem
        │
        ▼
 PROJECT MEMORY
```

Essa correspondência é extremamente forte.

No OSINT, o objeto central é um **caso investigativo**.

No nosso sistema, o objeto central deveria ser uma:

# Change

Não um chat.

Não uma sessão Claude.

Não um prompt.

Não um branch.

Uma **mudança de software formalmente rastreável**.

---

# 2. Isso muda até como devemos pensar o produto

O desenvolvedor poderia iniciar:

```bash
tool change new BUG-1842
```

E nascer:

```text
changes/
└── BUG-1842/
    ├── intake
    ├── context
    ├── impact
    ├── specification
    ├── plan
    ├── implementation
    ├── verification
    ├── reviews
    ├── approvals
    └── postmortem
```

Cada comando transforma um estado conhecido em outro estado conhecido.

Exatamente a boa ideia do projeto OSINT.

---

# 3. Mas temos uma oportunidade que o OSINT Framework não explora direito

O framework de OSINT usa muito:

```text
arquivo → agente → arquivo → agente → arquivo
```

Nós podemos formalizar isso.

Cada etapa pode ter:

```text
Input Contract
↓
Execution
↓
Output Contract
↓
Validation
↓
Evidence
↓
State transition
```

Ou seja:

```text
SPECIFICATION_CREATED
        │
        ▼
PLAN_ALLOWED
        │
        ▼
PLAN_APPROVED
        │
        ▼
IMPLEMENTATION_ALLOWED
```

Isso começa a parecer uma **state machine de engenharia**.

E eu acho que deveria ser.

O agente não decide:

> "acho que agora vou implementar".

O Workflow Engine decide se aquela capability está disponível.

---

# 4. Ports & Adapters entra perfeitamente aqui

Essa decisão sua eu considero correta.

O core **não pode saber o que é Claude, Codex, Gemini, Ollama ou qualquer provider específico**.

Conceitualmente:

```text
                    ┌─────────────┐
                    │ Domain/Core │
                    └──────┬──────┘
                           │
                      AI Provider Port
                           │
        ┌──────────────────┼──────────────────┐
        │                  │                  │
        ▼                  ▼                  ▼
 Anthropic Adapter   OpenAI Adapter     Gemini Adapter
        │                  │                  │
     Claude              Codex             Gemini
```

A interface poderia representar coisas como:

```go
type ModelProvider interface {
    Execute(ctx Context, request Request) (Response, error)
    Capabilities() Capabilities
}
```

Mas até isso provavelmente é simplista demais.

Porque um provider pode suportar:

* tool calling;
* structured output;
* reasoning;
* image input;
* huge context;
* MCP;
* code execution;
* caching;
* streaming.

Então nosso Agent Router deveria trabalhar com **capabilities**, não apenas nomes de modelos.

Por exemplo:

```yaml
role: architecture-reviewer

requires:
  structured_output: true
  context_window: ">=200k"
  tool_use: false
  code_reasoning: high
```

O runtime encontra um provider compatível.

Isso já permite algo fantástico:

> **workflow independente de fornecedor.**

Claude hoje.

Codex amanhã.

Modelo interno da empresa depois.

Modelo local em ambiente air-gapped eventualmente.

---

# 5. E Ports & Adapters não deveria valer apenas para IA

Eu faria disso um princípio arquitetural maior.

Ports para:

```text
AI Provider
Git Provider
SCM Provider
Issue Tracker
Static Analyzer
Test Runner
Security Scanner
Secrets Scanner
Artifact Store
Memory Store
Embedding Provider
Policy Engine
Sandbox
Telemetry
Identity Provider
```

Adapters:

```text
AI
 ├ OpenAI
 ├ Anthropic
 ├ Google
 ├ Ollama
 └ OpenRouter

SCM
 ├ GitHub
 ├ GitLab
 ├ Bitbucket
 └ local git

Issues
 ├ Jira
 ├ GitHub Issues
 ├ Linear
 └ Azure DevOps

Security
 ├ Semgrep
 ├ CodeQL
 ├ Trivy
 └ custom
```

Isso pode transformar o produto em uma **plataforma**, em vez de uma CLI amarrada a três fornecedores.

---

# 6. Agora chegamos na parte muito interessante: memória

Sua ideia da memória separada é muito boa.

Mas eu mudaria um detalhe importante.

Você falou:

> projeto A tem um repositório separado de memória.

Sim.

Mas eu **não colocaria um arquivo gigante XML, JSON ou SQLite versionado pelo Git**.

Especialmente se N desenvolvedores escreverem simultaneamente.

Isso causaria sofrimento.

---

# 7. SQLite seria excelente — mas não como artefato Git canônico

SQLite é fantástico para a **representação local/indexada da memória**.

Ele oferece inclusive FTS5 para full-text search. Em WAL, leitores podem trabalhar simultaneamente com um escritor, embora continue existindo essencialmente um escritor por vez no WAL convencional. ([SQLite][1])

Então localmente:

```text
memory repository
       │
       ▼
local index
       │
       ▼
SQLite
 ├ records
 ├ relationships
 ├ FTS
 ├ provenance
 ├ tags
 └ embeddings metadata
```

Excelente.

Mas versionar:

```text
memory.db
```

diretamente no Git?

Eu evitaria.

Git consegue armazenar binário, obviamente.

Mas merge semântico de banco SQLite seria péssimo.

Dois devs:

```text
Pedro → memory.db changed
Ana   → memory.db changed
```

Resultado:

💥.

---

# 8. XML também tem problema semelhante

XML tem propriedades legais:

* schema;
* namespaces;
* validação;
* estrutura;
* ferramentas maduras.

Mas imagine N desenvolvedores modificando:

```text
project-memory.xml
```

Você cria um enorme hotspot de merge.

Além disso, a hipótese:

> XML necessariamente consome menos tokens ou é melhor para LLM

não deve ser assumida.

Resultados variam por tokenizer, modelo e estrutura. Comparações recentes mostram que JSON, YAML, Markdown e XML possuem trade-offs diferentes, não um vencedor universal. ([Nathaniel Thomas][2])

Eu separaria duas coisas.

### Storage representation

O formato ideal para máquina/Git.

### Prompt representation

O formato ideal para aquela IA.

Não precisam ser iguais.

Essa separação é extremamente poderosa.

---

# 9. Então sua ideia de conversão está certa — só inverteria a responsabilidade

Você imaginou:

```text
XML
↓
Markdown
↓
AI
↓
Markdown
↓
XML
```

Eu generalizaria:

```text
Canonical Memory Model
       │
       ├── Markdown projection
       ├── JSON projection
       ├── XML projection
       ├── compact projection
       └── provider-specific projection
```

Ou seja:

# Memory AST

A memória possui um modelo semântico interno.

O formato físico é apenas uma representação.

Algo como:

```go
type MemoryRecord struct {
    ID         MemoryID
    Type       MemoryType
    Scope      Scope
    Subject    Subject
    Statement  string
    Evidence   []EvidenceRef
    Provenance Provenance
    Lifecycle  Lifecycle
    Validity   Validity
}
```

A partir disso:

```text
Memory Record
   │
   ├── Serialize YAML
   ├── Serialize JSON
   ├── Serialize XML
   ├── Render Markdown
   └── Render Prompt
```

Agora não ficamos presos a nenhum formato.

---

# 10. E eu acho que encontrei uma representação Git melhor

Em vez de:

```text
memory.xml
```

teríamos:

```text
memory/
├── architecture/
├── domain/
├── conventions/
├── decisions/
├── components/
├── incidents/
├── regressions/
├── dependencies/
├── integrations/
├── testing/
├── security/
├── glossary/
├── debt/
├── exceptions/
└── audit/
```

E registros pequenos.

Por exemplo:

```text
memory/
architecture/
  MEM-01J8F2....yaml
  MEM-01J8F3....yaml
```

Outro developer cria:

```text
MEM-01J8G7....yaml
```

Não há conflito.

Isso é praticamente um:

# append-oriented memory store

sobre Git.

---

# 11. Podemos ir ainda mais longe: registros imutáveis

Isso seria muito poderoso.

Uma memória criada nunca é silenciosamente sobrescrita.

Se:

```text
MEM-103
```

dizia:

> sistema utiliza RabbitMQ para eventos de cobrança.

E migramos para Kafka.

Não alteramos `MEM-103`.

Criamos:

```text
MEM-291
supersedes: MEM-103
```

E:

```text
MEM-103
status: superseded
```

Ou até deixar o estado materializado derivável pelos eventos.

Isso nos dá uma espécie de:

# event sourcing de conhecimento

Muito interessante para auditoria.

---

# 12. Aqui esprememos mais uma lição do OSINT

O OSINT framework possui:

```text
source_cases
reinforcement_count
confidence_tier
```

A intenção é boa.

Mas o método tem um defeito:

**repetição ≠ verdade.**

Se uma conclusão errada aparece quatro vezes, ela vira "alta confiança".

No nosso sistema isso seria perigoso.

Então confidence precisa ser função de várias coisas.

Conceitualmente:

```text
confidence =
    evidence_strength
  × validation_level
  × recency
  × source_reliability
  × applicability
```

Não apenas:

```text
times_seen
```

---

# 13. Mais importante ainda: memória de software pode expirar

Esse é um problema enorme em sistemas RAG para código.

Memória diz:

> `PaymentService` chama diretamente Stripe.

Depois alguém refatora.

Agora:

```text
PaymentService
     ↓
PaymentGateway Port
     ↓
Stripe Adapter
```

A memória antiga virou mentira.

Logo cada memória precisa saber **de onde ela veio**.

Exemplo:

```yaml
id: MEM-892

type: architecture_fact

statement: >
  PaymentService depends on PaymentGateway,
  not directly on Stripe.

evidence:
  - file: internal/payment/service.go
    symbol: PaymentService
    commit: 928ab38
    fingerprint: sha256:...

validity:
  status: valid
  last_verified_commit: 928ab38
```

Se o arquivo muda:

```text
fingerprint mismatch
```

A memória vira:

```text
possibly_stale
```

Isso é MUITO importante.

---

# 14. Essa pode virar uma feature central

# Memory Invalidation Engine

Mudou:

```text
internal/payment/service.go
```

O engine procura:

```text
memories referencing that artifact
```

E marca:

```text
MEM-892 → needs_revalidation
MEM-901 → needs_revalidation
MEM-933 → unaffected
```

Depois uma pipeline pode revalidar apenas as afetadas.

Isso transforma memória de IA em algo muito mais confiável.

---

# 15. Precisamos de tipos diferentes de memória

Outra coisa que o OSINT Framework simplifica demais.

Eu vejo pelo menos:

```text
Session Memory
Change Memory
Project Memory
Organization Memory
Developer Memory
```

Cada uma serve para uma coisa.

---

## Session Memory

Efêmera.

Só aquela execução.

```text
current reasoning context
temporary discoveries
tool outputs
scratch information
```

Morre depois.

---

## Change Memory

Relacionada à `BUG-1842`.

```text
requirements
impact
decisions
implementation details
failed approaches
review findings
```

Pode terminar promovendo conhecimento.

---

## Project Memory

Conhecimento durável:

```text
architecture
domain
conventions
dependencies
integrations
known pitfalls
testing strategy
historical regressions
```

Essa é sua memória compartilhada pelos N desenvolvedores.

---

## Organization Memory

Conhecimento transversal.

Por exemplo:

> Toda API externa da empresa usa OAuth2 Client Credentials.

Ou:

> bibliotecas X estão proibidas.

Isso pode atender 200 projetos.

---

## Developer Memory

Aqui eu teria muito cuidado.

Preferências pessoais:

```text
editor
output verbosity
preferred review style
```

Mas jamais permitiria que decisões pessoais contaminassem automaticamente Project Memory.

---

# 16. Promoção de memória

Essa é outra lição excelente do OSINT.

O postmortem não deveria simplesmente despejar tudo na memória global.

Nosso fluxo poderia ser:

```text
Observation
     ↓
Candidate Memory
     ↓
Validation
     ↓
Project Memory
```

Algo como:

```text
PROPOSED
   ↓
VALIDATED
   ↓
ACTIVE
   ↓
STALE
   ↓
DEPRECATED
```

E também:

```text
DISPUTED
REJECTED
SUPERSEDED
```

Isso é importante demais.

---

# 17. Um agente não deveria poder criar "verdades"

Ele pode propor:

```yaml
status: proposed
created_by:
  type: ai_agent
```

Mas determinadas categorias exigem:

```text
human approval
```

Exemplo:

> Arquitetura exige Repository Pattern.

Isso não pode virar verdade porque Claude achou bonito.

Precisaria de:

```yaml
approved_by:
  - pedro@example
```

ou proveniência equivalente pela identidade corporativa.

---

# 18. Auditoria: aqui sua ideia fica excelente

Você falou:

> comando tal, executado por fulano, gerou memória tal.

Sim.

Eu faria um Audit Ledger.

Exemplo:

```yaml
event_id: EVT-91823

timestamp: ...

actor:
  type: human
  id: pedro

command:
  name: project.inspect
  version: 1.4.3

execution:
  run_id: RUN-192

provider:
  name: anthropic
  model: claude-...

inputs:
  repository_commit: 8ab23de
  memory_snapshot: MEMSNAP-283

outputs:
  - MEM-901
  - MEM-902
  - ART-283

changes:
  added:
    - MEM-901
  superseded:
    - MEM-712
```

Agora temos:

> quem?

> quando?

> com qual comando?

> com qual versão do software?

> com qual modelo?

> contra qual commit?

> usando qual contexto?

> produzindo qual memória?

Isso é auditabilidade de verdade.

---

# 19. E eu colocaria hash em tudo

O Audit Ledger deveria carregar fingerprints.

```text
prompt/spec hash
workflow hash
policy hash
model identifier
input commit SHA
output SHA
memory snapshot SHA
```

Assim conseguimos reproduzir **o ambiente lógico** que levou à decisão.

Não necessariamente reproduzir byte-a-byte um LLM não determinístico.

Mas reconstruir o contexto.

---

# 20. Agora sua ideia do layoff fica tecnicamente muito forte

Você falou algo essencial:

> se amanhã substituirmos todos os desenvolvedores, o conhecimento permanece.

Isso é diferente de documentação tradicional.

Porque normalmente temos:

```text
README
Confluence
Jira
Slack
cabeça do João
cabeça da Maria
commits
PRs
```

fragmentados.

Nosso projeto tenta construir:

# Institutional Engineering Memory

E isso pode ser um dos valores empresariais mais fortes do produto.

Um desenvolvedor novo entra:

```bash
tool project attach payments
```

O runtime:

```text
clone project
↓
resolve memory repository
↓
checkout memory
↓
build local index
↓
validate freshness
↓
identify stale memories
↓
load architecture summary
↓
ready
```

Ele não recebe magicamente dez anos de experiência.

Mas começa muito acima de zero.

---

# 21. E não precisamos colocar a memória no repositório da aplicação

Concordo totalmente.

Podemos ter:

```text
github.com/company/payment-service

github.com/company/payment-service-memory
```

Ou:

```text
company-memory/
  projects/
```

Dependendo da estratégia.

O produto mantém uma associação:

```text
Project ID
   │
   ├ repository: git@.../payment-service
   └ memory:     git@.../payment-service-memory
```

---

# 22. Pequeno detalhe: eu evitaria chamar isso tecnicamente de Git submodule neste momento

Porque um Git submodule propriamente dito deixa uma referência no superproject através de `.gitmodules` e de um `gitlink` apontando para um commit específico. ([Git][3])

Você explicitamente quer independência entre os dois.

Então eu provavelmente faria:

```text
workspace/
├── source/
│   └── payment-service/
│
└── memory/
    └── payment-service-memory/
```

Ambos Git repos.

Relacionados pelo nosso Project Registry.

Não necessariamente pelo mecanismo Git Submodule.

Isso evita contaminar o source repository com conhecimento sobre nossa ferramenta.

Muito melhor.

---

# 23. GitHub CLI ainda pode automatizar tudo

Por exemplo:

```bash
tool project init .
```

detecta:

```text
git remote
organization
repository
```

E pergunta/policy determina:

```text
memory backend: github
```

O Git Adapter cria:

```text
payment-service.ai-memory
```

Ou outra convenção.

Configuração **local da ferramenta**, não necessariamente commitada na aplicação.

---

# 24. E multi-dev?

Aqui precisamos pensar bastante.

Se N pessoas trabalham ao mesmo tempo:

```text
Pedro ─────┐
Ana ───────┤
Lucas ─────┼──► Project Memory
Maria ─────┤
João ──────┘
```

Não podemos trabalhar com um arquivo monolítico.

Por isso registros independentes são importantes.

Cada run cria arquivos novos.

Merge:

```text
memory/add/MEM-A.yaml
memory/add/MEM-B.yaml
```

quase sempre trivial.

Para alterações conflitantes, o próprio sistema pode gerar uma:

# Memory Conflict

Exemplo:

Pedro:

> PaymentService chama Stripe diretamente.

Ana:

> PaymentService usa PaymentGateway.

O sistema detecta contradição semântica:

```text
MEMORY CONFLICT

Subject:
PaymentService.external_payment_dependency

A:
StripeClient

B:
PaymentGateway

Human resolution required.
```

Isso seria espetacular.

---

# 25. Git merge resolve texto. Nós podemos resolver conhecimento.

Essa frase resume uma possibilidade muito interessante.

Git sabe:

```text
linha A ≠ linha B
```

Nosso Memory Engine pode saber:

```text
assertion A contradicts assertion B
```

Então poderíamos ter:

```text
Memory Merge Engine
```

com:

* syntactic conflict;
* semantic conflict;
* duplicate knowledge;
* supersession;
* stale knowledge;
* scope conflict.

---

# 26. Mas cuidado: não devemos jogar toda memória na IA

Essa é uma armadilha enorme.

Você falou:

> IA conseguir ter noção de todo contexto.

Conceitualmente sim.

Fisicamente no prompt:

**não.**

Se projeto tiver 10 anos:

```text
100k memories
```

não enviamos 100k.

O Memory Engine precisa montar:

# Context Pack

Para uma tarefa.

Exemplo:

```text
BUG-1842
↓
affected domain: subscription
↓
dependency closure
↓
relevant memories
↓
relevant ADRs
↓
relevant regressions
↓
relevant policies
↓
token budget
↓
CONTEXT PACK
```

A IA recebe:

```text
3% da memória total
```

mas os **3% certos**.

---

# 27. Long-term memory não é prompt

Essa distinção precisa entrar no manifesto.

> **Long-term memory is a retrievable knowledge system, not accumulated prompt text.**

Isso é crucial.

---

# 28. Podemos usar várias estratégias de retrieval

Primeira etapa:

```text
deterministic retrieval
```

Por:

* path;
* symbol;
* domain;
* component;
* dependency;
* tags;
* change history.

Depois:

```text
FTS
```

SQLite FTS5 serviria maravilhosamente localmente. ([SQLite][4])

Depois, talvez:

```text
semantic retrieval
```

Embeddings.

E então:

```text
reranking
```

Mas eu **não começaria** embeddings-first.

Para código, relações estruturais frequentemente são muito mais relevantes do que similaridade semântica.

---

# 29. Imagine uma mudança em `PaymentService`

Primeiro:

```text
symbol lookup
```

Depois:

```text
dependency graph
```

Depois:

```text
memory subjects involving PaymentService
```

Depois:

```text
ADRs touching payments
```

Depois:

```text
previous regressions in payment
```

Depois:

```text
semantic retrieval
```

Essa ordem é muito mais inteligente.

---

# 30. Outra lição espremida do OSINT: gaps

O framework OSINT é bom em falar:

> não sei.

Isso deveria existir aqui.

O Repository Intelligence poderia produzir:

```yaml
knowledge_gaps:

  - subject: payment retry semantics
    reason: no tests and inconsistent implementations

  - subject: ownership
    reason: CODEOWNERS absent

  - subject: architecture
    reason: cyclic dependency detected
```

Assim a IA não tenta inventar conhecimento ausente.

---

# 31. E confidence

Cada memória:

```yaml
confidence: high
```

mas por quê?

Precisamos explicar.

```yaml
confidence:
  level: high

  basis:
    static_analysis: true
    runtime_test: true
    human_validated: true
    documentation_only: false
```

Muito melhor que um número mágico.

---

# 32. Fatos e decisões são coisas diferentes

Isso será fundamental.

Por exemplo:

```text
FACT
PaymentService currently imports StripeClient.
```

versus:

```text
DECISION
PaymentService must depend on PaymentGateway.
```

versus:

```text
OBSERVATION
Direct Stripe access appears in three legacy modules.
```

versus:

```text
POLICY
New code must not directly import Stripe SDK.
```

versus:

```text
HYPOTHESIS
Legacy retries may cause duplicate charges.
```

Não podemos misturar.

O OSINT Framework já tenta separar fato/inferência/hipótese.

Nós podemos tornar isso um **tipo do domínio**.

---

# 33. Nosso Memory Domain começa a aparecer

Tipos possíveis:

```text
Fact
Decision
Policy
Constraint
Convention
Observation
Hypothesis
Incident
Regression
Workaround
Debt
Risk
Ownership
Dependency
Integration
DomainKnowledge
Glossary
Runbook
Exception
```

Isso é muito mais rico que:

```text
memory.md
```

---

# 34. E cada tipo pode ter sua própria política de expiração

`Decision`

pode permanecer indefinidamente até superseded.

`Fact`

precisa ser revalidado quando source muda.

`Dependency`

expira quando manifest muda.

`Ownership`

expira quando CODEOWNERS muda.

`Regression`

é histórico — não expira.

`Policy`

depende de versão/aprovação.

Isso é muito sofisticado e muito útil.

---

# 35. Outra ideia poderosa: Memory Snapshot

Toda execução começa contra:

```text
Source Commit:
abc123

Memory Snapshot:
mem998
```

Então a execução é definida por:

```text
Code State + Memory State + Workflow State + Policy State
```

Isso é excelente para auditoria.

---

# 36. E aí chegamos em algo quase equivalente ao Git para conhecimento

Não literalmente.

Mas conceitualmente:

```text
source control
+
knowledge control
```

Git controla:

```text
code evolution
```

Nosso Memory Engine controla:

```text
engineering understanding evolution
```

---

# 37. Isso também resolve um problema enorme das ferramentas atuais

Claude Code aprende alguma coisa durante uma sessão.

Você fecha.

Outra pessoa abre.

Acabou.

Algumas ferramentas possuem persistent instructions ou memory.

Mas isso normalmente é:

```text
CLAUDE.md
AGENTS.md
rules/
```

São informações estáticas.

O que estamos falando é diferente:

```text
living engineering knowledge graph
```

produzido pelas próprias atividades do projeto.

---

# 38. O postmortem se torna vital

Lembra que o OSINT possui `/postmortem`?

Nós deveríamos ter algo talvez ainda mais importante.

Depois de uma PR:

```text
Was the change accepted?
Were review comments raised?
Were tests missed?
Was a regression detected later?
Was a policy violated?
Did implementation touch unexpected files?
Was memory inaccurate?
```

Isso alimenta o sistema.

Por exemplo:

```text
Regression:
NullPointerException after order cancellation.

Root cause:
AI changed shared mapper.

Existing memory:
No warning about mapper shared usage.

Action:
Create memory + propose policy.
```

Esse ciclo é ouro.

---

# 39. Podemos aprender com PR rejeitada

Isso seria uma inovação interessante.

Imagine:

```text
Agent produces code
↓
Human reviewer rejects
```

Não jogar isso fora.

Capturar:

```text
review reason
```

Por exemplo:

> "Nesse projeto nunca fazemos acesso ao repository diretamente no controller."

Isso vira:

```text
Candidate Convention
```

Depois de aprovação:

```text
Project Convention
```

No futuro:

```text
context pack
```

já contém isso.

Ou até:

```text
architecture policy
```

---

# 40. A ferramenta começa a aprender a empresa — sem treinar modelo

Exatamente como OSINT faz, porém melhor.

Não treinamos Claude.

Construímos uma:

# External Engineering Cognition Layer

Provider é substituível.

A memória continua.

Isso é incrivelmente importante.

---

# 41. Imagine trocar OpenAI por Anthropic amanhã

Nada é perdido.

Porque conhecimento não vive:

```text
inside model
```

Ele vive:

```text
Project Memory
```

Claude usa.

Codex usa.

Gemini usa.

Modelo interno usa.

Isso reforça ainda mais sua decisão de Ports & Adapters.

---

# 42. Agora temos três ativos independentes

```text
SOURCE
   │
   │
MEMORY
   │
   │
WORKFLOW
```

E providers de IA são recursos utilizados sobre esses ativos.

Isso é uma arquitetura muito saudável.

---

# 43. Segurança de memória

Tem outra coisa que precisamos colocar desde agora.

Memory repository pode conter:

* architecture;
* vulnerabilities;
* endpoints internos;
* nomes de serviços;
* infra;
* incidentes;
* decisões;
* talvez fragments de código.

Então memória é **material sensível**.

Precisamos desde o início de:

```text
Secret detection
PII detection
Classification
Encryption strategy
Access control
Retention
Audit
Redaction
```

Nunca permitir:

```text
AWS_SECRET_ACCESS_KEY=...
```

virar memória.

Mesmo que LLM diga:

> “isso parece importante”.

---

# 44. E provider boundary precisa respeitar classificação

Por exemplo:

```yaml
memory:
  classification: confidential

ai_policy:
  allowed_providers:
    - enterprise-anthropic

  forbidden:
    - public-openrouter
```

Então Agent Router considera também:

```text
data governance
```

Não somente capacidade técnica.

Isso torna Ports & Adapters ainda mais interessante para enterprise.

---

# 45. Temos outra ideia que nasceu diretamente dessa conversa

# Provider Trust Levels

```text
LOCAL
ENTERPRISE
EXTERNAL_APPROVED
EXTERNAL_RESTRICTED
FORBIDDEN
```

Uma mudança pode exigir:

```yaml
data_classification: restricted

provider_requirement:
  max_trust_boundary: enterprise
```

O runtime fisicamente não envia contexto a provider proibido.

Novamente:

**policy enforced**, não prompt.

---

# 46. Agora, sobre "memória de todos os devs"

Eu faria uma correção conceitual.

Não é:

> memória dos desenvolvedores.

É:

> **memória institucional do projeto derivada do trabalho dos desenvolvedores.**

Isso é muito melhor.

A autoria existe.

Mas conhecimento pertence ao projeto.

```text
Pedro discovered X
Ana validated X
Lucas superseded X
```

O projeto sabe X.

---

# 47. Isso permite conhecimento coletivo sem autoridade coletiva irrestrita

Porque podemos ter RBAC:

```text
Developer
  propose memory

Senior
  validate convention

Architect
  approve architecture decision

Security
  approve security policy

Admin
  manage providers
```

Isso pode ser configurável.

---

# 48. E auditoria pode ser append-only

Eu evitaria permitir:

```text
delete audit event
```

Operações corretivas produzem novos eventos.

```text
EVT-1 created memory
EVT-2 marked invalid
```

Nunca apagamos `EVT-1`.

Esse princípio é importante.

---

# 49. Podemos inclusive assinar eventos

Mais tarde, enterprise:

```text
signed commits
```

ou:

```text
cryptographic event signatures
```

E vincular:

```text
GitHub identity
OIDC
corporate SSO
```

Não é V0.

Mas a arquitetura não deve impedir.

---

# 50. Agora veja o que aconteceu

Inicialmente sua ideia era:

> ferramenta que controla IA desenvolvendo código.

Depois:

> spec-driven + policies + agents.

Agora temos:

```text
Governed AI Software Engineering Runtime
          +
Institutional Engineering Memory
          +
Software Change Provenance
```

Isso é **muito mais interessante**.

---

# 51. Eu colocaria cinco pilares provisórios

### 1. Developer Sovereignty

IA nunca é autoridade final.

### 2. Controlled Change

Toda mudança possui escopo, impacto, execução e verificação.

### 3. Evidence over Confidence

Não acreditamos porque o modelo disse.

Exigimos evidência.

### 4. Institutional Memory

Conhecimento pertence ao projeto, não à sessão nem ao provider.

### 5. Provider Independence

Nenhuma inteligência específica controla nossa arquitetura.

Esses cinco já parecem manifesto.

---

# 52. E tem um sexto

### Minimal Necessary Change

Especialmente para legado:

> **faça a menor mudança capaz de satisfazer a especificação preservando os invariantes conhecidos.**

Eu considero isso extremamente forte.

---

# 53. E um sétimo

### Knowledge Must Be Revisable

Nenhuma memória é verdade eterna.

Todo conhecimento tem:

```text
source
scope
time
validity
provenance
lifecycle
```

Isso resolveria uma das maiores fraquezas atuais dos sistemas de memória LLM.

---

# 54. A arquitetura conceitual agora já está ficando assim

```text
                             ENGINEER
                                │
                                ▼
                         ┌──────────────┐
                         │     CLI      │
                         └──────┬───────┘
                                │
                         ┌──────▼───────┐
                         │ Workflow     │
                         │ Engine       │
                         └──────┬───────┘
                                │
        ┌───────────────────────┼───────────────────────┐
        │                       │                       │
        ▼                       ▼                       ▼
 ┌────────────┐          ┌─────────────┐         ┌─────────────┐
 │ Spec Engine│          │Policy Engine│         │Memory Engine│
 └────────────┘          └─────────────┘         └──────┬──────┘
                                                       │
                                                 Project Memory
                                                       │
                                                   Git Remote
                                                       │
                                                   Local Index
                                                       │
                                                    SQLite
                                │
                                ▼
                         Repository Model
                                │
                                ▼
                          Impact Engine
                                │
                                ▼
                           Agent Router
                       ┌────────┼─────────┐
                       ▼        ▼         ▼
                    Claude    Codex     Gemini
                                │
                                ▼
                         Sandbox Runtime
                                │
                                ▼
                           Git Change
                                │
                                ▼
                       Verification Engine
                         │    │    │    │
                        AST  Test SAST Policy
                                │
                                ▼
                          Review Engine
                                │
                                ▼
                          Human Approval
                                │
                                ▼
                           Audit Ledger
                                │
                                ▼
                            Postmortem
                                │
                                ▼
                        Memory Candidates
```

A partir daqui já dá para conversar sobre um **sistema**, não mais apenas uma ideia.

---

# 55. E ainda existe outra consequência fantástica

Como cada projeto possui memória independente:

```text
Project A Memory
Project B Memory
Project C Memory
```

mas temos também:

```text
Organization Memory
```

podemos fazer promoção controlada:

```text
Project A learns rule
       │
       ▼
candidate organizational knowledge
       │
       ▼
human approval
       │
       ▼
Organization Memory
       │
       ├── Project A
       ├── Project B
       └── Project C
```

Exemplo:

Três projetos descobrem que uma biblioteca interna tem determinada armadilha.

Isso pode virar:

```text
organization-wide engineering knowledge
```

É exatamente a ideia de `global/playbooks` do OSINT Framework, mas com governança muito melhor.

---

# 56. E o fluxo inverso também existe

Organization Memory determina:

```text
Never use Log4j version < X.
```

Todos os projetos recebem.

Projeto A pode adicionar:

```text
Exception approved until DATE.
```

Ou seja:

```text
global policy
+
project override
+
exception
```

Isso começa a parecer software de engenharia corporativa de verdade.

---

# 57. Uma decisão que eu NÃO tomaria ainda: XML

Não quero matar sua ideia porque a linha de raciocínio de separar storage/prompt está certíssima.

Mas **XML especificamente eu deixaria em aberto**.

Primeiro construiremos um benchmark real:

```text
JSON
YAML
XML
Markdown
TOML
custom compact format
```

Usando providers:

```text
OpenAI
Anthropic
Gemini
local
```

Medindo:

```text
tokens
parse accuracy
generation accuracy
round-trip fidelity
latency
schema adherence
cost
```

Depois escolhemos.

Talvez descubramos:

```text
canonical = YAML
prompt = Markdown
```

ou:

```text
canonical = JSON
prompt = compact Markdown
```

ou algo totalmente diferente.

A arquitetura deve permitir descobrir empiricamente.

---

# 58. Mas eu já tenho uma preferência arquitetural

Hoje eu tenderia a:

```text
Canonical Git Storage:
small immutable YAML/JSON records

Local Query Projection:
SQLite + FTS

AI Projection:
Markdown / provider optimized

Optional Semantic Index:
vector store
```

Isso nos dá:

**Git-friendly + query-friendly + LLM-friendly**.

Não tentamos fazer um formato servir para três problemas incompatíveis.

---

# 59. E existe uma regra que eu colocaria desde o V0

> **LLMs never write canonical project memory directly.**

Fluxo:

```text
LLM
↓
Memory Candidate
↓
Schema validation
↓
Conflict detection
↓
Policy validation
↓
possibly human approval
↓
Canonical Memory
```

Esse detalhe é extremamente importante.

---

# 60. Exatamente a mesma coisa para código

O agente não deveria editar o working tree principal diretamente.

Idealmente:

```text
agent sandbox
↓
patch
↓
validate
↓
apply
```

Ou isolated worktree/container.

Então toda alteração é:

```text
proposed change
```

antes de ser:

```text
accepted change
```

A mesma filosofia para **código e conhecimento**.

Isso deixa a arquitetura belíssima:

```text
AI proposes.

System validates.

Human governs.
```

Esse talvez seja o mantra do projeto.

---

Portanto, sim: **esprememos bastante mais do OSINT-Agentic-Framework agora**, e a principal coisa que extraímos não é “multiagent”. É a arquitetura epistemológica dele: casos produzem evidências, evidências produzem conhecimento, conhecimento pode ser promovido, e tudo deveria permanecer rastreável.

Só que no nosso sistema estamos indo além ao adicionar **validade temporal, invalidação por mudança de código, memória tipada, proveniência forte, estados de lifecycle, snapshots, conflitos semânticos, promoção project→organization, armazenamento Git-native, índice local, context packs e isolamento do provider**.

E eu quero preservar tudo isso para a documentação futura em **arc42 + C4 Models + docToolchain**: as decisões já estão começando a formar ADRs naturais — Ports & Adapters, provider independence, external project memory, canonical-vs-projection separation, append-oriented audit, developer sovereignty e memory lifecycle são candidatas claras.

[1]: https://www.sqlite.org/wal.html?utm_source=chatgpt.com "Write-Ahead Logging"
[2]: https://nathom.github.io/llm-data-formats/?utm_source=chatgpt.com "Comparing Structured Data Formats for LLMs | Nathaniel Thomas"
[3]: https://git-scm.com/docs/git-submodule?utm_source=chatgpt.com "Git - git-submodule Documentation"
[4]: https://www.sqlite.org/fts5.html?utm_source=chatgpt.com "SQLite FTS5 Extension"

---

O Software será construido em cima da seguinte arquitetura:

- Arquitetura em camadas
- Ports and Adapters

Ele deverá respeitar principios como:

- SOLID
- KISS
- YAGNI
- DRY
...

É extremamente importante que não se tenha abreviações de palavras dentro do código, pois isso dificulta a legibilidade, como:

c -> context

Não pode ter esse tipo de situação!

Utilizaremos golang.





