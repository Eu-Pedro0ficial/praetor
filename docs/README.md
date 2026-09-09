# Praetor Documentation

Este diretório contém a documentação oficial do projeto Praetor.

## Estrutura

- `architecture/`
  Arquitetura oficial do sistema em arc42 + C4 Models,
  preparada para docToolchain.

- `product/`
  Product thesis, manifesto, visão, escopo e posicionamento.

- `research/`
  Pesquisa, insumos históricos, referências e estudos comparativos.

- `roadmap/`
  Roadmap V2, contratos de milestones, dependências, decisões abertas e
  planejamento histórico explicitamente arquivado.

- `decisions/`
  Decisões complementares que ainda não tenham sido promovidas
  para ADRs da arquitetura.

- `assets/`
  Imagens e demais recursos documentais.

A documentação arquitetural é uma fonte normativa do projeto. O ledger
canônico de rastreabilidade inversa fica em
`architecture/src/docs/arc42/appendices/traceability.adoc`; o roadmap resume
essa autoridade sem manter uma matriz paralela.

Código gerado ou escrito para o Praetor não deve contradizer
uma decisão arquitetural aceita sem que exista uma nova decisão
documentada que a substitua.
