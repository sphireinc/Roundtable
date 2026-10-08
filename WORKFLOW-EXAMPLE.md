                     HUMAN
                       |
                       v
                ROUNDTABLE CHAIR
                       |
              Task decomposition
                       |
             +---------+---------+
             |         |         |
             v         v         v
         Architect  Implementer  Tester
             |         |         |
             +---------+---------+
                       |
                SHARED STATE
                   SQLite
                       |
             Resource acquisition
                       |
                PATCH PROPOSAL
                       |
             +---------+---------+
             |         |         |
             v         v         v
          Reviewer   Security   Architect
             |         |         |
             +---------+---------+
                       |
                 POLICY GATES
                       |
                Human approval
                 (if required)
                       |
               TRANSACTION MANAGER
                       |
                Repository change
                       |
                Durable history
